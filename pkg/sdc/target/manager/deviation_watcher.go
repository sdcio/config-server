/*
Copyright 2026 Nokia.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package targetmanager

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	configv1alpha1apply "github.com/sdcio/config-server/pkg/generated/applyconfiguration/config/v1alpha1"
	"github.com/sdcio/data-server/pkg/api"
	logf "github.com/sdcio/logger"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	unManagedConfigDeviation = "__"
	fieldManagerDeviation    = "ConfigController"
)

type DeviationWatcher struct {
	key    storebackend.Key // target key
	client client.Client    // k8 client
	source DeviationSource  // data-server deviation watch

	m      sync.RWMutex
	cancel context.CancelFunc
}

func NewDeviationWatcher(
	key storebackend.Key,
	client client.Client,
	source DeviationSource,
) *DeviationWatcher {
	return &DeviationWatcher{
		key:    key,
		client: client,
		source: source,
	}
}

// We prefix the deviation with a prefix to avoid collision between config deviation names
// and target deviation names
func (r *DeviationWatcher) getTargetDeviationName() string {
	return configv1alpha1.DeviationName(configv1alpha1.DeviationType_TARGET, r.key.Name)
}

func (r *DeviationWatcher) getTargetDeviationKey() types.NamespacedName {
	return types.NamespacedName{
		Namespace: r.key.Namespace,
		Name:      r.getTargetDeviationName(),
	}
}

func (r *DeviationWatcher) Stop(ctx context.Context) {
	r.m.Lock()
	defer r.m.Unlock()
	if r.cancel == nil {
		return
	}

	r.clearTargetDeviation(ctx)

	log := logf.FromContext(ctx).WithValues("name", "targetDeviationWatcher", "target", r.key.String())
	log.Info("stop deviationWatcher")
	r.cancel()
	r.cancel = nil
}

func (r *DeviationWatcher) Start(ctx context.Context) {
	r.Stop(ctx)
	// don't lock before since stop also locks
	r.m.Lock()
	defer r.m.Unlock()
	ctx, r.cancel = context.WithCancel(ctx)
	go r.start(ctx)
}

func (r *DeviationWatcher) start(ctx context.Context) {
	log := logf.FromContext(ctx).WithValues("name", "targetDeviationWatcher", "target", r.key.String())
	log.Info("start deviationWatcher")
	for ctx.Err() == nil {
		events, err := r.source.WatchDeviations(ctx, r.key.String())
		if err != nil {
			if ctx.Err() == nil {
				log.Error(err, "cannot subscribe")
			}
		} else {
			r.consume(ctx, events)
			log.V(logf.VDebug).Info("deviation watch closed, resubscribing")
		}
		// resilience for a datastore that went away: resubscribe after a pause
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
}

// consume folds the events of one watch into deviations and processes them
// at the end of each run. It returns when the channel closes or ctx ends.
func (r *DeviationWatcher) consume(ctx context.Context, events <-chan api.DeviationEvent) {
	log := logf.FromContext(ctx).WithValues("name", "targetDeviationWatcher", "target", r.key.String())
	started := false
	// key is intent key,
	deviations := map[string][]api.DeviationEvent{}
	for {
		var ev api.DeviationEvent
		var ok bool
		select {
		case <-ctx.Done():
			return
		case ev, ok = <-events:
			if !ok {
				return
			}
		}
		switch ev.Kind {
		case api.DeviationStart:
			if started {
				log.Error(nil, "protocol violation start on start")
				continue
			}
			deviations = make(map[string][]api.DeviationEvent, 0)
			// set the unmanaged devidations to 0 upon start; if no unmanaged deviations are reported
			// this will reset the unmanaged deviations.
			deviations[unManagedConfigDeviation] = make([]api.DeviationEvent, 0)
			started = true
		case api.DeviationUpdate:
			if !started {
				log.Error(nil, "protocol violation update without start")
				continue
			}
			intent := ev.Intent
			// override intentName if it is unhandled -> this is an unmanaged intent
			if ev.Reason == api.DeviationReasonUnhandled {
				intent = unManagedConfigDeviation
			}
			if _, ok := deviations[intent]; !ok {
				deviations[intent] = make([]api.DeviationEvent, 0)
			}
			// do not append the devations with Intent exist data as they are used
			// to indicate an intent exist and will allow to clear the deviations
			// when no other deviations are reported.
			if ev.Reason == api.DeviationReasonIntentExists {
				continue
			}
			deviations[intent] = append(deviations[intent], ev)
		case api.DeviationEnd:
			if !started {
				log.Error(nil, "protocol violation end without start")
				continue
			}
			started = false
			r.processDeviations(ctx, deviations) // Process & clear deviations
			deviations = make(map[string][]api.DeviationEvent, 0)
		case api.DeviationClear:
			// manage them in batches going fwd, not implemented right now
			deviations = make(map[string][]api.DeviationEvent, 0)
		default:
			log.Info("unexecpted deviation event", "event", ev.Kind)
		}
	}
}

func (r *DeviationWatcher) processDeviations(ctx context.Context, deviations map[string][]api.DeviationEvent) {
	log := logf.FromContext(ctx)
	log.Info("process deviations")
	for configName, devs := range deviations {
		if configName == "default" {
			continue
		}
		configDevs := convertDeviationEvents(devs)

		nsn := r.getTargetDeviationKey()
		typ := configv1alpha1.DeviationType_CONFIG
		if configName == unManagedConfigDeviation {
			log.Info("target device deviations", "devs", len(configDevs))
			typ = configv1alpha1.DeviationType_TARGET
		} else {
			parts := strings.SplitN(configName, ".", 2)
			nsn = types.NamespacedName{
				Namespace: parts[0],
				Name:      configv1alpha1.DeviationName(configv1alpha1.DeviationType_CONFIG, parts[1]),
			}
			if len(parts) != 2 {
				log.Error(nil, "unexpected configName", "got", configName)
				return
			}
			log.Info("config deviations", "nsn", nsn, "devs", len(configDevs))
		}
		r.processDSDeviations(ctx, nsn, configDevs, typ)
	}
}

func (r *DeviationWatcher) processDSDeviations(
	ctx context.Context,
	nsn types.NamespacedName,
	deviations []configv1alpha1.ConfigDeviation,
	typ configv1alpha1.DeviationType,
) {
	log := logf.FromContext(ctx)

	sort.Slice(deviations, func(i, j int) bool {
		return deviations[i].Path < deviations[j].Path
	})

	applyDeviation := configv1alpha1apply.Deviation(nsn.Name, nsn.Namespace).
		WithLabels(map[string]string{
			config.TargetNameKey:      r.key.Name,
			config.TargetNamespaceKey: r.key.Namespace,
		}).
		WithSpec(configv1alpha1apply.DeviationSpec().
			WithDeviationType(typ).
			WithDeviations(configDeviationsToApply(deviations)...),
		)

	log.Info("patch deviations", "nsn", nsn, "devs", len(deviations))

	if err := r.client.Apply(ctx, applyDeviation, &client.ApplyOptions{
		FieldManager: fieldManagerDeviation,
		Force:        ptr.To(true),
	}); err != nil {
		log.Error(err, "cannot update intent for received deviation", "config", nsn)
	}
}

// Clear the Config deviation when we stop the discovery
// use the Deviation name with prefix to avoid name collision with the config type
func (r *DeviationWatcher) clearTargetDeviation(
	ctx context.Context,
) {
	log := logf.FromContext(ctx)

	nsn := r.getTargetDeviationKey()
	deviation := &configv1alpha1.Deviation{}
	if err := r.client.Get(ctx, nsn, deviation); err != nil {
		log.Error(err, "cannot get intent for recieved deviation", "config", nsn)
		return
	}
	// If already clear, do nothing (avoids useless writes)
	if len(deviation.Spec.Deviations) == 0 {
		return
	}

	log.Info("clear deviations", "nsn", nsn)

	applyDeviation := configv1alpha1apply.Deviation(nsn.Name, nsn.Namespace).
		WithSpec(configv1alpha1apply.DeviationSpec())
		// empty spec with no deviations — SSA with Force will clear the field

	if err := r.client.Apply(ctx, applyDeviation, &client.ApplyOptions{
		FieldManager: fieldManagerDeviation,
	}); err != nil {
		log.Error(err, "cannot clear deviation", "config", nsn)
	}
}

// convertDeviationEvents maps update events to the Deviation CR entries.
func convertDeviationEvents(devs []api.DeviationEvent) []configv1alpha1.ConfigDeviation {
	deviations := make([]configv1alpha1.ConfigDeviation, 0, len(devs))
	for _, dev := range devs {
		for _, e := range dev.Entries {
			deviations = append(deviations, configv1alpha1.ConfigDeviation{
				Path:         e.Path,
				DesiredValue: deviationString(e.ExpectedText),
				ActualValue:  deviationString(e.CurrentText),
				Reason:       deviationReason(dev.Reason),
			})
		}
	}
	return deviations
}

// deviationString maps an absent value to nil.
func deviationString(v string) *string {
	if v == "" {
		return nil
	}
	return ptr.To(v)
}

// deviationReason keeps the reason names stored in the Deviation CR.
func deviationReason(r api.DeviationReason) string {
	switch r {
	case api.DeviationReasonUnhandled:
		return "UNHANDLED"
	case api.DeviationReasonNotApplied:
		return "NOT_APPLIED"
	case api.DeviationReasonOverruled:
		return "OVERRULED"
	case api.DeviationReasonIntentExists:
		return "INTENT_EXISTS"
	default:
		return "DR_UNKNOWN"
	}
}

func configDeviationsToApply(devs []configv1alpha1.ConfigDeviation) []*configv1alpha1apply.ConfigDeviationApplyConfiguration {
	result := make([]*configv1alpha1apply.ConfigDeviationApplyConfiguration, 0, len(devs))
	for _, d := range devs {
		a := configv1alpha1apply.ConfigDeviation().
			WithPath(d.Path).
			WithReason(d.Reason)
		if d.DesiredValue != nil {
			a = a.WithDesiredValue(*d.DesiredValue)
		}
		if d.ActualValue != nil {
			a = a.WithActualValue(*d.ActualValue)
		}
		result = append(result, a)
	}
	return result
}

package configserver

import (
	"context"
	"reflect"
	"testing"

	configv1alpha1 "github.com/iptecharch/config-server/apis/config/v1alpha1"
	"github.com/iptecharch/config-server/apis/generated/clientset/versioned/scheme"
	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	dsclient "github.com/iptecharch/config-server/pkg/sdc/dataserver/client"
	"github.com/iptecharch/config-server/pkg/store"
	"github.com/iptecharch/config-server/pkg/store/memory"
	"github.com/iptecharch/config-server/pkg/target"
	sdcpb "github.com/iptecharch/sdc-protos/sdcpb"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func getTestScheme() *runtime.Scheme {
	runScheme := runtime.NewScheme()
	_ = scheme.AddToScheme(runScheme)
	// add the core object to the scheme
	for _, api := range (runtime.SchemeBuilder{
		clientgoscheme.AddToScheme,
		invv1alpha1.AddToScheme,
		configv1alpha1.AddToScheme,
	}) {
		_ = api(runScheme)
	}
	return runScheme
}

func initFakeClient(ctx context.Context, scheme *runtime.Scheme, namespace string, targets map[string]bool) client.Client {
	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	for targetName, ready := range targets {
		client.Create(ctx, buildTestTarget(ctx, namespace, targetName, ready))
	}

	return client
}

func buildTestTarget(ctx context.Context, namespace, name string, ready bool) *invv1alpha1.Target {
	conditions := []invv1alpha1.Condition{
		{Condition: invv1alpha1.Ready().Condition},
		{Condition: invv1alpha1.DSReady().Condition},
	}
	if !ready {
		conditions = []invv1alpha1.Condition{
			{Condition: invv1alpha1.Ready().Condition},
			{Condition: invv1alpha1.DSFailed("testNotReady").Condition},
		}
	}
	return invv1alpha1.BuildTarget(
		metav1.ObjectMeta{Name: name, Namespace: namespace},
		invv1alpha1.TargetSpec{},
		invv1alpha1.TargetStatus{
			ConditionedStatus: invv1alpha1.ConditionedStatus{
				Conditions: conditions,
			},
		},
	)
}

func buildTestConfig(ctx context.Context, namespace, name, targetName string) *configv1alpha1.Config {
	return configv1alpha1.BuildConfig(
		metav1.ObjectMeta{
			Name: name, Namespace: namespace,
			Labels: map[string]string{
				configv1alpha1.TargetNameKey:      targetName,
				configv1alpha1.TargetNamespaceKey: namespace,
			},
		},
		configv1alpha1.ConfigSpec{},
		configv1alpha1.ConfigStatus{},
	)
}

func buildOptions(fieldSet, labelSet map[string]string) *internalversion.ListOptions {
	options := &internalversion.ListOptions{}
	if len(fieldSet) > 0 {
		options.FieldSelector = fields.SelectorFromSet(fieldSet)
	}
	if len(labelSet) > 0 {
		options.LabelSelector = labels.SelectorFromSet(labelSet)
	}
	return options
}

func initTargetDataStore(ctx context.Context, namespace string, targets map[string]bool) store.Storer[target.Context] {
	targetStore := memory.NewStore[target.Context]()

	dsClient := dsclient.NewFakeClient()
	for targetName := range targets {
		key := store.KeyFromNSN(types.NamespacedName{Name: targetName, Namespace: namespace})
		targetStore.Create(ctx, key, target.Context{
			Client: dsClient,
			DataStore: &sdcpb.CreateDataStoreRequest{
				Schema: &sdcpb.Schema{
					Name:    "x",
					Vendor:  "v",
					Version: "v1",
				},
			},
		})
	}
	return targetStore
}

func TestNewConfigProviderBasic(t *testing.T) {
	cases := map[string]struct {
		namespace   string
		targetReady bool
		configs     map[string]string
	}{
		"Normal": {
			namespace: "dummy",
			configs: map[string]string{
				"config1": "dev1",
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			scheme := getTestScheme()

			targets := map[string]bool{}
			for _, targetName := range tc.configs {
				targets[targetName] = true
			}

			client := initFakeClient(ctx, scheme, tc.namespace, targets)
			targetstore := initTargetDataStore(ctx, tc.namespace, targets)

			configs := []*configv1alpha1.Config{}
			for configName, targetName := range tc.configs {
				configs = append(configs, buildTestConfig(ctx, tc.namespace, configName, targetName))
			}
			config := configs[0]

			provider, err := NewConfigMemProvider(ctx, &configv1alpha1.Config{}, scheme, client, targetstore)
			if err != nil {
				t.Errorf("%s unexpected error\n%s", name, err.Error())
			}

			ctx = genericapirequest.WithNamespace(ctx, config.GetNamespace())
			_, err = provider.Get(ctx, config.GetName(), &metav1.GetOptions{})
			if err == nil {
				// error is expected
				t.Errorf("%s expecting an error, got nil\n", name)
			}
			_, err = provider.List(ctx, &internalversion.ListOptions{})
			if err != nil {
				// error is not expected
				t.Errorf("%s expecting no error, got\n%s", name, err.Error())
			}
			obj, err := provider.Create(ctx, config, nil, &metav1.CreateOptions{})
			if err != nil {
				// error is not expected
				t.Errorf("%s expecting no error, got\n%s", name, err.Error())
			}
			if config, ok := obj.(*configv1alpha1.Config); ok {
				if config.GetCondition(configv1alpha1.ConditionTypeReady).Status != metav1.ConditionTrue {
					t.Errorf("%s expecting a ready condition, got\n%v", name, config)
				}
			}
		})
	}
}

func TestNewConfigProviderList(t *testing.T) {
	cases := map[string]struct {
		namespace     string
		configs       map[string]string
		labelSet      labels.Set
		fieldSet      fields.Set
		expectedItems int
	}{
		"MixedMatch": {
			namespace: "dummy",
			configs: map[string]string{
				"config1": "dev1",
				"config2": "dev1",
				"config3": "dev2",
			},
			fieldSet: map[string]string{
				"metadata.namespace": "dummy",
			},
			labelSet: map[string]string{
				configv1alpha1.TargetNameKey: "dev1",
			},
			expectedItems: 2,
		},
		"FieldsOnly": {
			namespace: "dummy",
			configs: map[string]string{
				"config1": "dev1",
				"config2": "dev1",
				"config3": "dev2",
			},
			fieldSet: map[string]string{
				"metadata.namespace": "dummy",
			},
			expectedItems: 3,
		},
		"LabelsOnlyMixedMatch": {
			namespace: "dummy",
			configs: map[string]string{
				"config1": "dev1",
				"config2": "dev1",
				"config3": "dev2",
			},
			labelSet: map[string]string{
				configv1alpha1.TargetNameKey:      "dev1",
				configv1alpha1.TargetNamespaceKey: "dummy",
			},
			expectedItems: 2,
		},
		"LabelsOnlyNoMatch": {
			namespace: "dummy",
			configs: map[string]string{
				"config1": "dev1",
				"config2": "dev1",
				"config3": "dev2",
			},
			labelSet: map[string]string{
				configv1alpha1.TargetNameKey:      "dev1",
				configv1alpha1.TargetNamespaceKey: "x",
			},
			expectedItems: 0,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			scheme := getTestScheme()
			targets := map[string]bool{}
			for _, targetName := range tc.configs {
				targets[targetName] = true
			}

			client := initFakeClient(ctx, scheme, tc.namespace, targets)
			targetstore := initTargetDataStore(ctx, tc.namespace, targets)

			configs := []*configv1alpha1.Config{}
			for configName, targetName := range tc.configs {
				configs = append(configs, buildTestConfig(ctx, tc.namespace, configName, targetName))
			}

			provider, err := NewConfigMemProvider(ctx, &configv1alpha1.Config{}, scheme, client, targetstore)
			if err != nil {
				t.Errorf("%s unexpected error\n%s", name, err.Error())
			}

			for _, config := range configs {
				ctx = genericapirequest.WithNamespace(ctx, config.GetNamespace())
				obj, err := provider.Create(ctx, config, nil, &metav1.CreateOptions{})
				if err != nil {
					// error is not expected
					t.Errorf("%s expecting no error, got\n%s", name, err.Error())
				}
				if config, ok := obj.(*configv1alpha1.Config); ok {
					if config.GetCondition(configv1alpha1.ConditionTypeReady).Status != metav1.ConditionTrue {
						t.Errorf("%s expecting a ready condition, got\n%v", name, config)
					}
				}
			}

			obj, err := provider.List(ctx, buildOptions(tc.fieldSet, tc.labelSet))
			if err != nil {
				t.Errorf("%s expecting no error, got\n%s", name, err.Error())
			}
			if configList, ok := obj.(*configv1alpha1.ConfigList); ok {
				if len(configList.Items) != tc.expectedItems {
					t.Errorf("%s expecting %d, got %d", name, tc.expectedItems, len(configList.Items))
				}
			} else {
				t.Errorf("%s expecting a configlist, got\n%s", name, reflect.TypeOf(obj).Name())
				return
			}
		})
	}
}

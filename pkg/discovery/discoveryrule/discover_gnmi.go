/*
Copyright 2024 Nokia.

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

package discoveryrule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/henderiw/logger/log"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/gnmic/pkg/api"
	"github.com/openconfig/gnmic/pkg/api/path"
	"github.com/openconfig/gnmic/pkg/api/target"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (r *dr) discoverWithGNMI(ctx context.Context, h *hostInfo, connProfile *invv1alpha1.TargetConnectionProfile) error {
	log := log.FromContext(ctx)
	secret := &corev1.Secret{}
	err := r.client.Get(ctx, types.NamespacedName{
		Namespace: r.cfg.CR.GetNamespace(),
		Name:      r.cfg.DiscoveryProfile.Secret,
	}, secret)
	if err != nil {
		return err
	}
	address := fmt.Sprintf("%s:%d", h.Address, connProfile.Spec.Port)

	t, err := createGNMITarget(ctx, address, secret, connProfile)
	if err != nil {
		return err
	}
	log.Debug("Creating gNMI client")
	err = t.CreateGNMIClient(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err := t.Close(); err != nil {
			log.Error("closing gNMI target", "err", err)
		}
	}()
	capRsp, err := t.Capabilities(ctx)
	if err != nil {
		return err
	}
	discoverer, err := r.getDiscovererGNMI(capRsp)
	if err != nil {
		return err
	}
	di, err := discoverer.Discover(ctx, t)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(di)
	log.Info("discovery info", "info", string(b))

	return r.createTarget(ctx, discoverer.GetProvider(), h.Address, di)
}

func createGNMITarget(_ context.Context, address string, secret *corev1.Secret, connProfile *invv1alpha1.TargetConnectionProfile) (*target.Target, error) {
	tOpts := []api.TargetOption{
		//api.Name(req.NamespacedName.String()),
		api.Address(address),
		api.Username(string(secret.Data["username"])),
		api.Password(string(secret.Data["password"])),
		api.Timeout(5 * time.Second),
	}

	if connProfile.Spec.Insecure != nil && *connProfile.Spec.Insecure {
		tOpts = append(tOpts, api.Insecure(true))
	} else {
		tOpts = append(tOpts, api.SkipVerify(true))
	}
	// TODO: query certificate, its secret and use it
	return api.NewTarget(tOpts...)
}

func (r *dr) getDiscovererGNMI(capRsp *gnmi.CapabilityResponse) (*Discoverer, error) {
	for _, m := range capRsp.SupportedModels {
		for provider, discoveryParameters := range r.gnmiDiscoveryProfiles {
			if m.Organization == discoveryParameters.Organization {
				if discoveryParameters.ModelMatch == nil {
					return &Discoverer{
						Provider:            provider,
						DiscoveryParameters: discoveryParameters,
					}, nil
				} else {
					if strings.Contains(m.Name, *discoveryParameters.ModelMatch) {
						return &Discoverer{
							Provider:            provider,
							DiscoveryParameters: discoveryParameters,
						}, nil
					}
				}
			}
		}
	}
	return nil, errors.New("unknown target vendor")
}

type Discoverer struct {
	Provider            string
	DiscoveryParameters invv1alpha1.GnmiDiscoveryVendorProfileParameters
}

func (r *Discoverer) GetProvider() string {
	return r.Provider
}

func (r *Discoverer) Discover(ctx context.Context, t *target.Target) (*configv1alpha1.DiscoveryInfo, error) {
	var req *gnmi.GetRequest
	var err error
	switch r.DiscoveryParameters.GetEncoding() {
	case "JSON_IETF":
		req, err = api.NewGetRequest(
			api.EncodingJSON_IETF(),
		)
	case "JSON":
		req, err = api.NewGetRequest(
			api.EncodingJSON(),
		)
	case "PROTO":
		req, err = api.NewGetRequest(
			api.EncodingPROTO(),
		)
	case "ASCII":
		req, err = api.NewGetRequest(
			api.EncodingASCII(),
		)
	default:
		return nil, fmt.Errorf("unsupported encoding, got: %s", string(r.DiscoveryParameters.GetEncoding()))
	}
	if err != nil {
		return nil, err
	}

	// Ensure req.Path is initialized
	req.Path = make([]*gnmi.Path, 0, len(r.DiscoveryParameters.Paths))

	// Convert paths into a map for quick lookup
	pathMap, err := getPathMap(req, r.DiscoveryParameters.Paths)
	if err != nil {
		return nil, err
	}
	capRsp, err := t.Capabilities(ctx)
	if err != nil {
		return nil, err
	}
	getRsp, err := t.Get(ctx, req)
	if err != nil {
		return nil, err
	}

	return r.parseDiscoveryInformation(ctx, pathMap, capRsp, getRsp)
}

func getPathMap(req *gnmi.GetRequest, discoveryPaths []invv1alpha1.DiscoveryPathDefinition) (map[string]invv1alpha1.DiscoveryPathDefinition, error) {
	pathMap := make(map[string]invv1alpha1.DiscoveryPathDefinition)
	for _, param := range discoveryPaths {
		parsedPath, err := path.ParsePath(param.Path)
		if err != nil {
			return nil, fmt.Errorf("invalid GNMI path %q: %w", param.Path, err)
		}
		req.Path = append(req.Path, parsedPath)

		gnmiPath := GnmiPathToXPath(parsedPath, true)
		pathMap[gnmiPath] = param // Store the key for fast lookup
	}
	return pathMap, nil
}

func (r *Discoverer) parseDiscoveryInformation(
	ctx context.Context,
	pathMap map[string]invv1alpha1.DiscoveryPathDefinition,
	capRsp *gnmi.CapabilityResponse,
	getRsp *gnmi.GetResponse,
) (*configv1alpha1.DiscoveryInfo, error) {
	log := log.FromContext(ctx).With("provider", r.Provider)

	di := &configv1alpha1.DiscoveryInfo{
		Protocol:           string(invv1alpha1.Protocol_GNMI),
		Provider:           r.Provider,
		SupportedEncodings: make([]string, 0, len(capRsp.GetSupportedEncodings())),
	}
	for _, enc := range capRsp.GetSupportedEncodings() {
		di.SupportedEncodings = append(di.SupportedEncodings, enc.String())
	}

	// Define field mapping
	fieldMapping := map[string]*string{
		"version":      &di.Version,
		"platform":     &di.Platform,
		"serialNumber": &di.SerialNumber,
		"macAddress":   &di.MacAddress,
		"hostname":     &di.Hostname,
	}

	// Process gNMI notifications
	for _, notif := range getRsp.GetNotification() {
		for _, upd := range notif.GetUpdate() {
			gnmiPath := GnmiPathToXPath(upd.GetPath(), true)

			// If preserveNamespace is false and the path returned from the device
			// contains a leading namespace then we strip it
			preserveNamespace := r.DiscoveryParameters.GetPreserveNamespace()

			if !preserveNamespace && len(strings.Split(gnmiPath, ":")) > 1 {
				gnmiPath = strings.Join(strings.Split(gnmiPath, ":")[1:], ":")
			}

			log.Debug("discovery", "path", preserveNamespace)

			// SRLINUX a path that was requested without keys is returned as a JSON blob up to the first element
			// for which the first key was found
			// Right now we dont process this properly.
			// If you experience such behavior you should use another encoding

			if param, exists := pathMap[gnmiPath]; exists {
				if targetField, found := fieldMapping[param.Key]; found {
					log.Debug("discovery before transform", "path", gnmiPath, "key", param.Key, "value", upd.GetVal())

					string_value, err := getStringValue(upd.GetVal())
					if err != nil {
						log.Error("discovery unexpected value", "path", gnmiPath, "key", param.Key, "value", upd.GetVal())
					}

					*targetField = string_value

					log.Debug("discovery before transform", "path", gnmiPath, "key", param.Key, "value", *targetField)

					// Apply transformations (Regex + Starlark)
					transformedValue, err := applyTransformations(ctx, param, *targetField)
					if err != nil {
						return nil, fmt.Errorf("failed to process transformation for %q: %w", param.Key, err)
					}

					log.Debug("discovery after transform", "path", gnmiPath, "key", param.Key, "value", transformedValue)
					*targetField = transformedValue
				}
			}
		}
	}
	return di, nil
}

func applyTransformations(
	ctx context.Context,
	param invv1alpha1.DiscoveryPathDefinition,
	value string) (string, error) {
	var err error

	// Apply regex if provided
	if param.Regex != nil {
		value, err = ApplyRegex(*param.Regex, value)
		if err != nil {
			return "", fmt.Errorf("regex error: %w", err)
		}
	}

	// Apply Starlark script if provided
	if param.Script != nil {
		value, err = RunStarlark(param.Key, *param.Script, value)
		if err != nil {
			return "", fmt.Errorf("starlark error: %w", err)
		}
	}
	return value, nil
}

// RunStarlark executes a Starlark script with the given value.
func RunStarlark(key, script, value string) (string, error) {
	thread := &starlark.Thread{Name: "transformer"}

	starlarkTransformer, err := starlark.ExecFileOptions(&syntax.FileOptions{}, thread, "transformer.star", script, starlark.StringDict{})
	if err != nil {
		return "", fmt.Errorf("transformer %s err: %v", key, err)
	}
	transformer := starlarkTransformer["transform"]
	result, err := starlark.Call(thread, transformer, starlark.Tuple{starlark.Value(starlark.String(value))}, nil)
	if err != nil {
		// this is a starlark execution runtime failure
		return "", fmt.Errorf("starlark execution runtime failure: %s", err.Error())
	}

	// Convert result back to string
	if str, ok := result.(starlark.String); ok {
		return string(str), nil
	}
	return "", fmt.Errorf("unexpected Starlark return type: %T", result)
}

func ApplyRegex(pattern string, value string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex pattern: %w", err)
	}
	matches := re.FindStringSubmatch(value)
	if len(matches) > 1 {
		return matches[1], nil // Extract first capture group
	}
	return value, nil // No match, return original
}

// GnmiPathToXPath is absed on gnmic but also strips the json_ietf metadata
func GnmiPathToXPath(p *gnmi.Path, noKeys bool) string {
	if p == nil {
		return ""
	}
	sb := &strings.Builder{}
	if p.Origin != "" {
		sb.WriteString(p.Origin)
		sb.WriteString(":")
	}
	elems := p.GetElem()
	numElems := len(elems)

	for i, pe := range elems {
		split := strings.Split(pe.GetName(), ":")
		sb.WriteString(split[len(split)-1])

		if !noKeys {
			numKeys := len(pe.GetKey())
			switch numKeys {
			case 0:
			case 1:
				for k := range pe.GetKey() {
					writeKey(sb, k, pe.GetKey()[k])
				}
			default:
				keys := make([]string, 0, numKeys)
				for k := range pe.GetKey() {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					writeKey(sb, k, pe.GetKey()[k])
				}
			}
		}
		if i+1 != numElems {
			sb.WriteString("/")
		}
	}
	return sb.String()
}

func writeKey(sb *strings.Builder, k, v string) {
	sb.WriteString("[")
	sb.WriteString(k)
	sb.WriteString("=")
	sb.WriteString(v)
	sb.WriteString("]")
}

func getStringValue(updValue *gnmi.TypedValue) (string, error) {
	if updValue == nil {
		return "", fmt.Errorf("no value returned")
	}
	switch updValue.Value.(type) {
	case *gnmi.TypedValue_AsciiVal:
		return updValue.GetAsciiVal(), nil
	case *gnmi.TypedValue_BoolVal:
		return fmt.Sprintf("%t", updValue.GetBoolVal()), nil
	case *gnmi.TypedValue_BytesVal:
		return string(updValue.GetBytesVal()), nil
	case *gnmi.TypedValue_DecimalVal:
		return "", fmt.Errorf("decimal is depreciated")
	case *gnmi.TypedValue_FloatVal:
		//lint:ignore SA1019 still need GetFloatVal for backward compatibility
		return "", fmt.Errorf("float is depreciated")
	case *gnmi.TypedValue_DoubleVal:
		return fmt.Sprintf("%f", updValue.GetDoubleVal()), nil
	case *gnmi.TypedValue_IntVal:
		return fmt.Sprintf("%d", updValue.GetIntVal()), nil
	case *gnmi.TypedValue_StringVal:
		return updValue.GetStringVal(), nil
	case *gnmi.TypedValue_UintVal:
		return fmt.Sprintf("%d", updValue.GetUintVal()), nil
	case *gnmi.TypedValue_JsonIetfVal:
		return strings.Trim(string(updValue.GetJsonIetfVal()), "\""), nil
	case *gnmi.TypedValue_JsonVal:
		return strings.Trim(string(updValue.GetJsonVal()), "\""), nil
	case *gnmi.TypedValue_LeaflistVal:
		return fmt.Sprintf("%v", updValue.GetLeaflistVal()), nil
	case *gnmi.TypedValue_ProtoBytes:
		return string(updValue.GetProtoBytes()), nil
	case *gnmi.TypedValue_AnyVal:
		return fmt.Sprintf("%v", updValue.GetAnyVal()), nil
	default:
		return "", fmt.Errorf("unexpected type %s", reflect.TypeOf(updValue.Value).Name())
	}
}

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
	"reflect"
	"testing"

	"github.com/openconfig/gnmi/proto/gnmi"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
)

func TestParseDiscoveryInformation(t *testing.T) {
	cases := map[string]struct {
		capabilityFile string
		provider       string
		getResponse    func() *gnmi.GetResponse
		expectedResult *invv1alpha1.DiscoveryInfo
		expectError    bool
	}{
		"NokiaSRL": {
			capabilityFile: "data/nokia-srl-capabilities.json",
			provider:       "srl.nokia.sdcio.dev",
			getResponse:    getSRLResponse,
			expectedResult: &invv1alpha1.DiscoveryInfo{
				Protocol:           "gnmi",
				Provider:           "srl.nokia.sdcio.dev",
				Version:            "24.3.2",
				Hostname:           "edge02",
				Platform:           "7220 IXR-D2",
				MacAddress:         "1A:05:04:FF:00:00",
				SerialNumber:       "Sim Serial No.",
				SupportedEncodings: []string{"JSON_IETF", "PROTO", "ASCII", "52", "42", "43", "45", "44", "46", "47", "48", "49", "50", "53"},
			},
		},
		"Arista": {
			capabilityFile: "data/arista-capabilities.json",
			provider:       "eos.arista.sdcio.dev",
			getResponse:    getAristaResponse,
			expectedResult: &invv1alpha1.DiscoveryInfo{
				Protocol:           "gnmi",
				Provider:           "eos.arista.sdcio.dev",
				Version:            "4.33.1F",
				Hostname:           "edge02",
				Platform:           "cEOSLab",
				MacAddress:         "00:1c:73:6f:e4:7c",
				SerialNumber:       "864D1228500892CEF934F5C6DF8784B2",
				SupportedEncodings: []string{"JSON", "JSON_IETF", "ASCII"},
			},
		},
		"Cisco": {
			capabilityFile: "data/cisco-capabilities.json",
			provider:       "iosxr.cisco.sdcio.dev",
			getResponse:    getCiscoResponse,
			expectedResult: &invv1alpha1.DiscoveryInfo{
				Protocol:           "gnmi",
				Provider:           "iosxr.cisco.sdcio.dev",
				Version:            "24.4.1.26I",
				Hostname:           "edge02",
				Platform:           "Cisco IOS-XRv 9000 Centralized Virtual Router",
				MacAddress:         "02:42:0a:0a:14:65",
				SerialNumber:       "VSN-NPP2G7K",
				SupportedEncodings: []string{"JSON_IETF", "ASCII", "PROTO"},
			},
		},
	}

	profiles, err := LoadDiscoveryProfiles(DiscoveryVendorProfilePath)
	if err != nil {
		t.Fatalf("Failed to load discovery profiles: %v", err)
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			// Load gNMI capability response
			capRsp, err := getCapbilityResponse(tc.capabilityFile)
			if err != nil {
				t.Fatalf("Failed to load capability response: %v", err)
			}

			getRsp := tc.getResponse()

			// Convert paths into a map for quick lookup
			pathMap, err := getPathMap(&gnmi.GetRequest{}, profiles[tc.provider].Paths)
			if err != nil {
				t.Fatalf("Expected error: %v, got: %v", tc.expectError, err)
			}

			d := Discoverer{Provider: tc.provider}
			di, err := d.parseDiscoveryInformation(ctx, pathMap, capRsp, getRsp)
			// Check error conditions
			if (err != nil) != tc.expectError {
				t.Fatalf("Expected error: %v, got: %v", tc.expectError, err)
			}
			if err != nil {
				// If error is expected, we don't check the output further
				return
			}

			// Validate each field
			if di.Protocol != tc.expectedResult.Protocol {
				t.Errorf("Protocol mismatch: expected %s, got %s", tc.expectedResult.Protocol, di.Protocol)
			}
			if di.Provider != tc.expectedResult.Provider {
				t.Errorf("Provider mismatch: expected %s, got %s", tc.expectedResult.Provider, di.Provider)
			}
			if di.Version != tc.expectedResult.Version {
				t.Errorf("Version mismatch: expected %s, got %s", tc.expectedResult.Version, di.Version)
			}
			if di.Hostname != tc.expectedResult.Hostname {
				t.Errorf("HostName mismatch: expected %s, got %s", tc.expectedResult.Hostname, di.Hostname)
			}
			if di.Platform != tc.expectedResult.Platform {
				t.Errorf("Platform mismatch: expected %s, got %s", tc.expectedResult.Platform, di.Platform)
			}
			if di.MacAddress != tc.expectedResult.MacAddress {
				t.Errorf("MacAddress mismatch: expected %s, got %s", tc.expectedResult.MacAddress, di.MacAddress)
			}
			if di.SerialNumber != tc.expectedResult.SerialNumber {
				t.Errorf("SerialNumber mismatch: expected %s, got %s", tc.expectedResult.SerialNumber, di.SerialNumber)
			}
			if !reflect.DeepEqual(di.SupportedEncodings, tc.expectedResult.SupportedEncodings) {
				t.Errorf("SupportedEncodings mismatch: expected %v, got %v", tc.expectedResult.SupportedEncodings, di.SupportedEncodings)
			}
		})
	}
}

func getSRLResponse() *gnmi.GetResponse {
	return &gnmi.GetResponse{
		Notification: []*gnmi.Notification{
			{
				Update: []*gnmi.Update{
					{
						Path: &gnmi.Path{Elem: []*gnmi.PathElem{
							{Name: "srl_nokia-platform:platform"},
							{Name: "srl_nokia-platform-control:control", Key: map[string]string{"slot": "A"}},
							{Name: "software-version"},
						}},
						Val: &gnmi.TypedValue{
							Value: &gnmi.TypedValue_JsonIetfVal{JsonIetfVal: []byte("v24.3.2-118-g706b4f0d99")},
						},
					},
				},
			},
			{
				Update: []*gnmi.Update{
					{
						Path: &gnmi.Path{Elem: []*gnmi.PathElem{
							{Name: "srl_nokia-platform:platform"},
							{Name: "srl_nokia-platform-chassis:chassis"},
							{Name: "type"},
						}},
						Val: &gnmi.TypedValue{
							Value: &gnmi.TypedValue_JsonIetfVal{JsonIetfVal: []byte("7220 IXR-D2")},
						},
					},
				},
			},
			{
				Update: []*gnmi.Update{
					{
						Path: &gnmi.Path{Elem: []*gnmi.PathElem{
							{Name: "srl_nokia-system:system"},
							{Name: "srl_nokia-system-name:name"},
							{Name: "host-name"},
						}},
						Val: &gnmi.TypedValue{
							Value: &gnmi.TypedValue_JsonIetfVal{JsonIetfVal: []byte("edge02")},
						},
					},
				},
			},
			{
				Update: []*gnmi.Update{
					{
						Path: &gnmi.Path{Elem: []*gnmi.PathElem{
							{Name: "srl_nokia-platform:platform"},
							{Name: "srl_nokia-platform-chassis:chassis"},
							{Name: "serial-number"},
						}},
						Val: &gnmi.TypedValue{
							Value: &gnmi.TypedValue_JsonIetfVal{JsonIetfVal: []byte("Sim Serial No.")},
						},
					},
				},
			},
			{
				Update: []*gnmi.Update{
					{
						Path: &gnmi.Path{Elem: []*gnmi.PathElem{
							{Name: "srl_nokia-platform:platform"},
							{Name: "srl_nokia-platform-chassis:chassis"},
							{Name: "hw-mac-address"},
						}},
						Val: &gnmi.TypedValue{
							Value: &gnmi.TypedValue_JsonIetfVal{JsonIetfVal: []byte("1A:05:04:FF:00:00")},
						},
					},
				},
			},
		},
	}
}

func getAristaResponse() *gnmi.GetResponse {
	return &gnmi.GetResponse{
		Notification: []*gnmi.Notification{
			{
				Update: []*gnmi.Update{
					{
						Path: &gnmi.Path{Elem: []*gnmi.PathElem{
							{Name: "components"},
							{Name: "component", Key: map[string]string{"name": "EOS"}},
							{Name: "state"},
							{Name: "software-version"},
						}},
						Val: &gnmi.TypedValue{
							Value: &gnmi.TypedValue_StringVal{StringVal: "4.33.1F-39879738.4331F (engineering build)"},
						},
					},
				},
			},
			{
				Update: []*gnmi.Update{
					{
						Path: &gnmi.Path{Elem: []*gnmi.PathElem{
							{Name: "components"},
							{Name: "component", Key: map[string]string{"name": "Chassis"}},
							{Name: "state"},
							{Name: "part-no"},
						}},
						Val: &gnmi.TypedValue{
							Value: &gnmi.TypedValue_StringVal{StringVal: "cEOSLab"},
						},
					},
				},
			},
			{
				Update: []*gnmi.Update{
					{
						Path: &gnmi.Path{Elem: []*gnmi.PathElem{
							{Name: "system"},
							{Name: "state"},
							{Name: "hostname"},
						}},
						Val: &gnmi.TypedValue{
							Value: &gnmi.TypedValue_StringVal{StringVal: "edge02"},
						},
					},
				},
			},
			{
				Update: []*gnmi.Update{
					{
						Path: &gnmi.Path{Elem: []*gnmi.PathElem{
							{Name: "components"},
							{Name: "component", Key: map[string]string{"name": "Chassis"}},
							{Name: "state"},
							{Name: "serial-no"},
						}},
						Val: &gnmi.TypedValue{
							Value: &gnmi.TypedValue_StringVal{StringVal: "864D1228500892CEF934F5C6DF8784B2"},
						},
					},
				},
			},
			{
				Update: []*gnmi.Update{
					{
						Path: &gnmi.Path{Elem: []*gnmi.PathElem{
							{Name: "lldp"},
							{Name: "state"},
							{Name: "chassis-id"},
						}},
						Val: &gnmi.TypedValue{
							Value: &gnmi.TypedValue_StringVal{StringVal: "00:1c:73:6f:e4:7c"},
						},
					},
				},
			},
		},
	}
}

func getCiscoResponse() *gnmi.GetResponse {
	return &gnmi.GetResponse{
		Notification: []*gnmi.Notification{
			{
				Update: []*gnmi.Update{
					{
						Path: &gnmi.Path{Origin: "openconfig-system",
							Elem: []*gnmi.PathElem{
								{Name: "system"},
								{Name: "state"},
								{Name: "software-version"},
							}},
						Val: &gnmi.TypedValue{
							Value: &gnmi.TypedValue_StringVal{StringVal: "24.4.1.26I"},
						},
					},
				},
			},
			{
				Update: []*gnmi.Update{
					{
						Path: &gnmi.Path{Origin: "openconfig-platform",
							Elem: []*gnmi.PathElem{
								{Name: "components"},
								{Name: "component", Key: map[string]string{"name": "Rack 0"}},
								{Name: "state"},
								{Name: "part-no"},
							}},
						Val: &gnmi.TypedValue{
							Value: &gnmi.TypedValue_StringVal{StringVal: "Cisco IOS-XRv 9000 Centralized Virtual Router"},
						},
					},
				},
			},
			{
				Update: []*gnmi.Update{
					{
						Path: &gnmi.Path{Origin: "openconfig-system",
							Elem: []*gnmi.PathElem{
								{Name: "system"},
								{Name: "state"},
								{Name: "hostname"},
							}},
						Val: &gnmi.TypedValue{
							Value: &gnmi.TypedValue_StringVal{StringVal: "edge02"},
						},
					},
				},
			},
			{
				Update: []*gnmi.Update{
					{
						Path: &gnmi.Path{Origin: "openconfig-platform",
							Elem: []*gnmi.PathElem{
								{Name: "components"},
								{Name: "component", Key: map[string]string{"name": "Rack 0"}},
								{Name: "state"},
								{Name: "serial-no"},
							}},
						Val: &gnmi.TypedValue{
							Value: &gnmi.TypedValue_StringVal{StringVal: "VSN-NPP2G7K"},
						},
					},
				},
			},
			{
				Update: []*gnmi.Update{
					{
						Path: &gnmi.Path{Origin: "openconfig-interfaces",
							Elem: []*gnmi.PathElem{
								{Name: "interfaces"},
								{Name: "interface", Key: map[string]string{"name": "MgmtEth0/RP0/CPU0/0"}},
								{Name: "openconfig-if-ethernet:ethernet"},
								{Name: "state"},
								{Name: "mac-address"},
							}},
						Val: &gnmi.TypedValue{
							Value: &gnmi.TypedValue_StringVal{StringVal: "02:42:0a:0a:14:65"},
						},
					},
				},
			},
		},
	}
}

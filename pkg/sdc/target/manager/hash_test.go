package targetmanager

import (
	"reflect"
	"testing"

	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/sdcio/data-server/pkg/api"
	"github.com/stretchr/testify/assert"
)

func fullHashSpec() api.DatastoreSpec {
	tgt := func() api.Target {
		return api.Target{
			Type:        api.TargetTypeNetconf,
			Address:     "10.0.0.1",
			Port:        830,
			Credentials: &api.Credentials{Username: "u", Password: "p", Token: "t"},
			TLS:         &api.TLS{CA: "ca", Cert: "cert", Key: "key", SkipVerify: true},
			ProtocolOptions: api.ProtocolOptions{
				GNMI:    &api.GNMIOptions{Encoding: "JSON_IETF", TargetName: "tn"},
				Netconf: &api.NetconfOptions{IncludeNs: true, OperationWithNs: true, UseOperationRemove: true, CommitCandidate: api.CommitCandidateRunning},
			},
		}
	}
	syncTgt := tgt()
	return api.DatastoreSpec{
		Name:   "ds1",
		Target: tgt(),
		Schema: api.SchemaRef{Vendor: "nokia", Version: "24.3"},
		Sync: api.Sync{
			Configs: []api.SyncConfig{{Name: "c", Target: &syncTgt, Paths: []string{"/a", "/b"}, Mode: api.SyncModeSample, Interval: 10}},
		},
	}
}

func fullHashRefs() *configv1alpha1.TargetStatusUsedReferences {
	return &configv1alpha1.TargetStatusUsedReferences{
		ConnectionProfileResourceVersion: "1",
		SyncProfileResourceVersion:       "2",
		SecretResourceVersion:            "3",
		TLSSecretResourceVersion:         "4",
	}
}

func TestComputeCreateDSHashStable(t *testing.T) {
	a := ComputeCreateDSHash(fullHashSpec(), fullHashRefs())
	assert.Equal(t, a, ComputeCreateDSHash(fullHashSpec(), fullHashRefs()))
	assert.Len(t, a, 64)
}

// The golden value pins the canonical form: changing it recreates every datastore.
func TestComputeCreateDSHashGolden(t *testing.T) {
	const want = "88b3c79432d2f6591a4710c1ed958d9104076c20ee027197bb6d0164f5323260"
	assert.Equal(t, want, ComputeCreateDSHash(fullHashSpec(), fullHashRefs()))
}

// Every leaf field of the spec and the refs must influence the hash.
func TestComputeCreateDSHashChangesOnEveryField(t *testing.T) {
	spec, refs := fullHashSpec(), fullHashRefs()
	base := ComputeCreateDSHash(spec, refs)

	var walk func(path string, v reflect.Value)
	walk = func(path string, v reflect.Value) {
		switch v.Kind() {
		case reflect.Pointer:
			walk(path, v.Elem())
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				walk(path+"."+v.Type().Field(i).Name, v.Field(i))
			}
		case reflect.Slice:
			if v.Len() > 0 {
				walk(path+"[0]", v.Index(0))
			}
		default:
			orig := reflect.New(v.Type()).Elem()
			orig.Set(v)
			switch v.Kind() {
			case reflect.String:
				v.SetString(v.String() + "x")
			case reflect.Bool:
				v.SetBool(!v.Bool())
			case reflect.Int, reflect.Int32, reflect.Int64:
				v.SetInt(v.Int() + 1)
			case reflect.Uint32, reflect.Uint64:
				v.SetUint(v.Uint() + 1)
			default:
				t.Fatalf("%s: unhandled kind %s", path, v.Kind())
			}
			assert.NotEqual(t, base, ComputeCreateDSHash(spec, refs), "hash ignores %s", path)
			v.Set(orig)
		}
	}
	walk("spec", reflect.ValueOf(&spec).Elem())
	walk("refs", reflect.ValueOf(refs))
}

func TestComputeCreateDSHashNilVersusEmpty(t *testing.T) {
	a, b := fullHashSpec(), fullHashSpec()
	a.Target.Credentials = nil
	b.Target.Credentials = &api.Credentials{}
	assert.NotEqual(t, ComputeCreateDSHash(a, nil), ComputeCreateDSHash(b, nil), "credentials")

	s := fullHashSpec()
	assert.NotEqual(t, ComputeCreateDSHash(s, nil), ComputeCreateDSHash(s, &configv1alpha1.TargetStatusUsedReferences{}), "refs")
}

func TestComputeCreateDSHashRefBoundaries(t *testing.T) {
	s := fullHashSpec()
	a := &configv1alpha1.TargetStatusUsedReferences{SecretResourceVersion: "a", TLSSecretResourceVersion: "b"}
	b := &configv1alpha1.TargetStatusUsedReferences{SecretResourceVersion: "ab"}
	assert.NotEqual(t, ComputeCreateDSHash(s, a), ComputeCreateDSHash(s, b))
}

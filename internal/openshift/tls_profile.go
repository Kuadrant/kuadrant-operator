package openshift

import (
	"os"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

const DefaultAPIServerCRName = "cluster"

var (
	APIServerGVK = schema.GroupVersionKind{
		Group:   configv1.GroupName,
		Version: configv1.GroupVersion.Version,
		Kind:    "APIServer",
	}
	APIServerGroupKind = APIServerGVK.GroupKind()
	APIServersResource = configv1.SchemeGroupVersion.WithResource("apiservers")
)

// APIServerCRName returns the name of the OpenShift APIServer CR to read TLS
// configuration from. Defaults to "cluster". Override with the APISERVER_CR_NAME
// environment variable.
func APIServerCRName() string {
	if name := os.Getenv("APISERVER_CR_NAME"); name != "" {
		return name
	}
	return DefaultAPIServerCRName
}

func IsOpenShiftServerConfigInstalled(restMapper meta.RESTMapper) (bool, error) {
	return utils.IsCRDInstalled(restMapper, APIServerGVK.Group, APIServerGVK.Kind, APIServerGVK.Version)
}

func LinkKuadrantToAPIServer(objs controller.Store) machinery.LinkFunc {
	kuadrants := lo.Map(objs.FilterByGroupKind(kuadrantv1beta1.KuadrantGroupKind), controller.ObjectAs[machinery.Object])
	targetName := APIServerCRName()

	return machinery.LinkFunc{
		From: kuadrantv1beta1.KuadrantGroupKind,
		To:   APIServerGroupKind,
		Func: func(child machinery.Object) []machinery.Object {
			if child.GetName() != targetName {
				return nil
			}
			return kuadrants
		},
	}
}

var openshiftVersionMap = map[configv1.TLSProtocolVersion]string{
	configv1.VersionTLS10: "1.0",
	configv1.VersionTLS11: "1.1",
	configv1.VersionTLS12: "1.2",
	configv1.VersionTLS13: "1.3",
}

func openshiftVersionToShort(v configv1.TLSProtocolVersion) string {
	if s, ok := openshiftVersionMap[v]; ok {
		return s
	}
	return string(v)
}

// opensslToIANA maps OpenSSL-style cipher names used in OpenShift TLS profiles
// to Go/IANA-style names used by crypto/tls.
var opensslToIANA = map[string]string{
	// TLS 1.3 ciphers (same name in both conventions)
	"TLS_AES_128_GCM_SHA256":       "TLS_AES_128_GCM_SHA256",
	"TLS_AES_256_GCM_SHA384":       "TLS_AES_256_GCM_SHA384",
	"TLS_CHACHA20_POLY1305_SHA256": "TLS_CHACHA20_POLY1305_SHA256",

	// TLS 1.2 ECDHE ciphers
	"ECDHE-ECDSA-AES128-GCM-SHA256": "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
	"ECDHE-RSA-AES128-GCM-SHA256":   "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
	"ECDHE-ECDSA-AES256-GCM-SHA384": "TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384",
	"ECDHE-RSA-AES256-GCM-SHA384":   "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
	"ECDHE-ECDSA-CHACHA20-POLY1305": "TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256",
	"ECDHE-RSA-CHACHA20-POLY1305":   "TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
	"ECDHE-ECDSA-AES128-SHA256":     "TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256",
	"ECDHE-RSA-AES128-SHA256":       "TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256",
	"ECDHE-ECDSA-AES128-SHA":        "TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA",
	"ECDHE-RSA-AES128-SHA":          "TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA",
	"ECDHE-ECDSA-AES256-SHA384":     "TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA384",
	"ECDHE-RSA-AES256-SHA384":       "TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA384",
	"ECDHE-ECDSA-AES256-SHA":        "TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA",
	"ECDHE-RSA-AES256-SHA":          "TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA",

	// DHE ciphers (not supported by Go crypto/tls — skipped during translation)

	// Non-ECDHE ciphers
	"AES128-GCM-SHA256": "TLS_RSA_WITH_AES_128_GCM_SHA256",
	"AES256-GCM-SHA384": "TLS_RSA_WITH_AES_256_GCM_SHA384",
	"AES128-SHA256":     "TLS_RSA_WITH_AES_128_CBC_SHA256",
	"AES256-SHA256":     "TLS_RSA_WITH_AES_256_CBC_SHA256",
	"AES128-SHA":        "TLS_RSA_WITH_AES_128_CBC_SHA",
	"AES256-SHA":        "TLS_RSA_WITH_AES_256_CBC_SHA",
	"DES-CBC3-SHA":      "TLS_RSA_WITH_3DES_EDE_CBC_SHA",
}

// ResolveTLSProfileFromTopology navigates the policy machinery topology to find
// an APIServer child of the given Kuadrant object and resolves its TLS profile.
// Falls back to the default Intermediate profile when no APIServer CR is found
// or OpenShift server config is not installed.
func ResolveTLSProfileFromTopology(topology *machinery.Topology, kobj *kuadrantv1beta1.Kuadrant, isOpenShiftServerConfigInstalled bool) (string, []string) {
	if !isOpenShiftServerConfigInstalled {
		return ResolveTLSProfile(nil)
	}

	for _, child := range topology.Objects().Children(kobj) {
		if child.GroupVersionKind().GroupKind() != APIServerGroupKind {
			continue
		}
		rObj, ok := child.(*controller.RuntimeObject)
		if !ok {
			continue
		}
		apiServer, ok := rObj.Object.(*configv1.APIServer)
		if !ok {
			continue
		}
		return ResolveTLSProfile(apiServer.Spec.TLSSecurityProfile)
	}

	return ResolveTLSProfile(nil)
}

// ResolveTLSProfile resolves a TLSSecurityProfile into a min TLS version string
// and a list of IANA cipher suite names. If profile is nil, the Intermediate
// profile is used as default.
func ResolveTLSProfile(profile *configv1.TLSSecurityProfile) (minVersion string, cipherSuites []string) {
	var spec *configv1.TLSProfileSpec

	if profile != nil && profile.Type == configv1.TLSProfileCustomType {
		if profile.Custom != nil {
			spec = &profile.Custom.TLSProfileSpec
		}
	} else if profile != nil {
		spec = configv1.TLSProfiles[profile.Type]
	}

	if spec == nil {
		spec = configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
	}

	minVersion = openshiftVersionToShort(spec.MinTLSVersion)

	for _, opensslName := range spec.Ciphers {
		if ianaName, ok := opensslToIANA[opensslName]; ok {
			cipherSuites = append(cipherSuites, ianaName)
		}
	}

	return minVersion, cipherSuites
}

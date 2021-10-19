/*
 Copyright 2021 Red Hat, Inc.

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

package istioprovider

import (
	"net/http"
	"net/url"

	"github.com/getkin/kin-openapi/openapi3"
	"istio.io/api/networking/v1alpha3"
	gatewayapiv1alpha1 "sigs.k8s.io/gateway-api/apis/v1alpha1"

	networkingv1beta1 "github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
)

type PathMatchType string

// PathMatchType constants.
const (
	PathMatchExact             PathMatchType = "Exact"
	PathMatchPrefix            PathMatchType = "Prefix"
	PathMatchRegularExpression PathMatchType = "RegularExpression"
)

type HTTPRouteFactory struct {
	Name            string
	RewriteHost     *string
	RewriteURI      *string
	URIMatchPath    string
	URIMatchType    PathMatchType
	Method          string
	DestinationHost string
	DestinationPort uint32
}

func (h *HTTPRouteFactory) HTTPRoute() *v1alpha3.HTTPRoute {
	httpRoute := &v1alpha3.HTTPRoute{
		Name: h.Name,
		Match: []*v1alpha3.HTTPMatchRequest{
			{
				Uri: stringMatch(h.URIMatchPath, h.URIMatchType),
				Method: &v1alpha3.StringMatch{
					MatchType: &v1alpha3.StringMatch_Exact{Exact: h.Method},
				},
			},
		},
		Route: []*v1alpha3.HTTPRouteDestination{
			{
				Destination: &v1alpha3.Destination{
					Host: h.DestinationHost,
					Port: &v1alpha3.PortSelector{
						Number: h.DestinationPort,
					},
				},
			},
		},
	}

	// Here we are rewriting the auhtority of the request to one of the hosts in the API definition.
	// TODO(jmprusi): Is this something expected? should we allow for a host override?
	if h.RewriteHost != nil {
		if httpRoute.Rewrite == nil {
			httpRoute.Rewrite = &v1alpha3.HTTPRewrite{}
		}
		httpRoute.Rewrite.Authority = *h.RewriteHost
	}

	if h.RewriteURI != nil {
		if httpRoute.Rewrite == nil {
			httpRoute.Rewrite = &v1alpha3.HTTPRewrite{}
		}
		httpRoute.Rewrite.Uri = *h.RewriteURI
	}

	return httpRoute
}

func ConvertPathMatchType(matchType gatewayapiv1alpha1.PathMatchType) PathMatchType {
	switch matchType {
	case gatewayapiv1alpha1.PathMatchExact:
		return PathMatchExact
	case gatewayapiv1alpha1.PathMatchPrefix:
		return PathMatchPrefix
	default:
		return PathMatchRegularExpression
	}
}

func stringMatch(path string, matchType PathMatchType) *v1alpha3.StringMatch {
	switch matchType {
	case PathMatchExact:
		return &v1alpha3.StringMatch{
			MatchType: &v1alpha3.StringMatch_Exact{Exact: path},
		}
	case PathMatchPrefix:
		return &v1alpha3.StringMatch{
			MatchType: &v1alpha3.StringMatch_Prefix{Prefix: path},
		}
	default:
		return &v1alpha3.StringMatch{
			MatchType: &v1alpha3.StringMatch_Regex{Regex: path},
		}
	}
}

func HTTPRoutesFromOAS(oasContent string, pathPrefix *string, destination networkingv1beta1.Destination) ([]*v1alpha3.HTTPRoute, error) {
	doc, err := openapi3.NewLoader().LoadFromData([]byte(oasContent))
	if err != nil {
		return nil, err
	}

	var rewriteHost *string
	if len(doc.Servers) > 0 {
		oasURL, err := url.Parse(doc.Servers[0].URL)
		if err != nil {
			return nil, err
		}
		rewriteHost = &oasURL.Host
	}

	httpRoutes := []*v1alpha3.HTTPRoute{}
	for path, pathItem := range doc.Paths {
		for opVerb, operation := range pathItem.Operations() {
			//TODO(jmprusi): Right now we are ignoring the security field of the operation, we should review this.

			factory := HTTPRouteFactory{
				Name:         operation.OperationID,
				RewriteHost:  rewriteHost,
				URIMatchPath: path,
				URIMatchType: PathMatchExact,
				Method:       opVerb,
				// TODO(jmprusi): Get the actual internal cluster hostname instead of hardcoding it.
				DestinationHost: destination.Name + "." + destination.Namespace + ".svc.cluster.local",
				DestinationPort: uint32(*destination.Port),
			}

			// Handle Prefix Override.
			if pathPrefix != nil {
				// We need to rewrite the path, to match what the service expects, basically,
				// removing the prefixOverride
				factory.RewriteURI = &path
				// If there's an Override, lets append it to the actual Operation Path.
				factory.URIMatchPath = *pathPrefix + path
			}

			httpRoutes = append(httpRoutes, factory.HTTPRoute())
		}
	}

	return httpRoutes, nil
}

func HTTPRoutesFromPath(pathMatch *gatewayapiv1alpha1.HTTPPathMatch, pathPrefix *string, destination networkingv1beta1.Destination) ([]*v1alpha3.HTTPRoute, error) {
	if pathMatch == nil {
		return nil, nil
	}

	// Allow any valid HTTP method
	methods := []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace,
	}

	httpRoutes := make([]*v1alpha3.HTTPRoute, 0, len(methods))

	for _, method := range methods {
		factory := HTTPRouteFactory{
			Name:         method,
			URIMatchPath: *pathMatch.Value,
			URIMatchType: ConvertPathMatchType(*pathMatch.Type),
			Method:       method,
			// TODO(jmprusi): Get the actual internal cluster hostname instead of hardcoding it.
			DestinationHost: destination.Name + "." + destination.Namespace + ".svc.cluster.local",
			DestinationPort: uint32(*destination.Port),
		}

		// Handle Prefix Override.
		if pathPrefix != nil {
			// We need to rewrite the path, to match what the service expects, basically,
			// removing the prefixOverride
			factory.RewriteURI = pathMatch.Value
			// If there's an Override, lets append it to the actual Operation Path.
			factory.URIMatchPath = *pathPrefix + *pathMatch.Value
		}

		httpRoutes = append(httpRoutes, factory.HTTPRoute())
	}

	return httpRoutes, nil
}

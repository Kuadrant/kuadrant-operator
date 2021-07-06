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
	"net/url"

	"github.com/getkin/kin-openapi/openapi3"
	"istio.io/api/networking/v1alpha3"

	networkingv1beta1 "github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
)

type HTTPRouteFactory struct {
	OperationID     string
	APIHost         string
	MatchPath       string
	OpVerb          string
	DestinationHost string
	DestinationPort uint32
	RewriteURI      *string
}

func (h *HTTPRouteFactory) HTTPRoute() *v1alpha3.HTTPRoute {
	httpRoute := &v1alpha3.HTTPRoute{
		Name: h.OperationID,
		// Here we are rewriting the auhtority of the request to one of the hosts in the API definition.
		// TODO(jmprusi): Is this something expected? should we allow for a host override?
		Rewrite: &v1alpha3.HTTPRewrite{Authority: h.APIHost},
		Match: []*v1alpha3.HTTPMatchRequest{
			{
				Uri: &v1alpha3.StringMatch{
					MatchType: &v1alpha3.StringMatch_Prefix{Prefix: h.MatchPath},
				},
				Method: &v1alpha3.StringMatch{
					MatchType: &v1alpha3.StringMatch_Exact{Exact: h.OpVerb},
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

	if h.RewriteURI != nil {
		httpRoute.Rewrite.Uri = *h.RewriteURI
	}

	return httpRoute
}

func HTTPRoutesFromOAS(doc *openapi3.T, pathPrefix string, tag networkingv1beta1.Tag) ([]*v1alpha3.HTTPRoute, error) {
	// TODO(jmprusi): Getting one of the hosts from the OpenAPISpec... extract this logic and improve.
	oasURL, err := url.Parse(doc.Servers[0].URL)
	if err != nil {
		return nil, err
	}

	httpRoutes := []*v1alpha3.HTTPRoute{}
	for path, pathItem := range doc.Paths {
		for opVerb, operation := range pathItem.Operations() {
			//TODO(jmprusi): Right now we are ignoring the security field of the operation, we should review this.

			factory := HTTPRouteFactory{
				OperationID: operation.OperationID,
				APIHost:     oasURL.Host,
				MatchPath:   path,
				OpVerb:      opVerb,
				// TODO(jmprusi): Get the actual internal cluster hostname instead of hardcoding it.
				DestinationHost: tag.Destination.Name + "." + tag.Destination.Namespace + ".svc.cluster.local",
				DestinationPort: uint32(*tag.Destination.Port),
			}

			// Handle Prefix Override.
			if pathPrefix != "" {
				// We need to rewrite the path, to match what the service expects, basically,
				// removing the prefixOverride
				factory.RewriteURI = &path
				// If there's an Override, lets append it to the actual Operation Path.
				factory.MatchPath = pathPrefix + path
			}

			httpRoutes = append(httpRoutes, factory.HTTPRoute())
		}
	}

	return httpRoutes, nil
}

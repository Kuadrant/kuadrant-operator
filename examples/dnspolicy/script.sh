export DNSPOLICY_NAMESPACE=gateway
kubectl create ns gateway
envsubst < examples/dnspolicy/aws-dns-provider-secret.yaml | kubectl apply -f -
envsubst < examples/dnspolicy/gateway.yaml | kubectl apply -f -
envsubst < examples/dnspolicy/dnspolicy.yaml | kubectl apply -f -
envsubst < examples/dnspolicy/application.yaml | kubectl apply -f -

read -r -p "press enter to cause conflict"
export DNSPOLICY_NAMESPACE=gateway-2
kubectl create ns gateway-2
envsubst < examples/dnspolicy/aws-dns-provider-secret.yaml | kubectl apply -f -
envsubst < examples/dnspolicy/gateway.yaml | kubectl apply -f -
envsubst < examples/dnspolicy/dnspolicy-bad-strategy.yaml | kubectl apply -f -
envsubst < examples/dnspolicy/application.yaml | kubectl apply -f -

read -r -p "press enter to delete conflict"
kubectl delete ns gateway-2

read -r -p "press enter to configure bad health checks"
export DNSPOLICY_NAMESPACE=gateway
envsubst < examples/dnspolicy/dnspolicy-healthchecks.yaml | kubectl apply -f -

read -r -p "press enter to configure good health checks"
kubectl patch dnspolicy prod-web -n ${DNSPOLICY_NAMESPACE} --type='json' -p='[{"op": "replace", "path": "/spec/healthCheck/port", "value":80}]'

read -r -p "press enter to clean up sample"
kubectl delete ns ${DNSPOLICY_NAMESPACE}

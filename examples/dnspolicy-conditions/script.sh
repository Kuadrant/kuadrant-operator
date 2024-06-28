export DNSPOLICY_NAMESPACE=gateway
kubectl create ns gateway
envsubst < examples/dnspolicy-conditions/managedzone.yaml | kubectl apply -f -
envsubst < examples/dnspolicy-conditions/gateway.yaml | kubectl apply -f -
envsubst < examples/dnspolicy-conditions/dnspolicy.yaml | kubectl apply -f -
envsubst < examples/dnspolicy-conditions/application.yaml | kubectl apply -f -

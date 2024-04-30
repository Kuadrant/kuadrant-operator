export DNSPOLICY_NAMESPACE=gateway
kubectl create ns gateway
envsubst < examples/dnspolicy/managedzone.yaml | kubectl apply -f -
envsubst < examples/dnspolicy/gateway.yaml | kubectl apply -f -
envsubst < examples/dnspolicy/dnspolicy.yaml | kubectl apply -f -
envsubst < examples/dnspolicy/application.yaml | kubectl apply -f -

read -r -p "press enter to cause conflict"
export DNSPOLICY_NAMESPACE=gateway-2
kubectl create ns gateway-2
envsubst < examples/dnspolicy/managedzone.yaml | kubectl apply -f -
envsubst < examples/dnspolicy/gateway.yaml | kubectl apply -f -
envsubst < examples/dnspolicy/dnspolicy.yaml | kubectl apply -f -
envsubst < examples/dnspolicy/application.yaml | kubectl apply -f -

read -r -p "press enter to delete conflict"
kubectl delete ns gateway-2

read -r -p "press enter to clean up sample"
kubectl delete ns gateway
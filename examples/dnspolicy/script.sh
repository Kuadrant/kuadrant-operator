kubectl create ns gateway
envsubst < examples/dnspolicy/managedzone.yaml | kubectl apply -f -
envsubst < examples/dnspolicy/gateway.yaml | kubectl apply -f -
envsubst < examples/dnspolicy/dnspolicy.yaml | kubectl apply -f -
envsubst < examples/dnspolicy/application.yaml | kubectl apply -f -

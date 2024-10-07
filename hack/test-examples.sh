#!/usr/bin/env bash
set -eu -o pipefail

parallel=10
# Load the configmaps that contains the parameter values used for certain examples.
kubectl apply -f examples/configmaps/simple-parameters-configmap.yaml

testworkflows=($(grep -LR 'workflows.argoproj.io/do-not-test' examples/*.yaml | tr '\n' ' '))
# If arguments, test those files instead
if [ $# -gt 0 ]; then
   testworkflows=("$@")
fi

exitcode=0
# echo "Checking for banned images..."
# for f in "${testworkflows[@]}"; do
#   echo " - $f"
#   if [ 0 != "$(grep -o 'image: .*' "$f" | grep -cv 'argoproj/argosay:v2\|python:alpine3.6\|busybox\|alpine:latest\|argoproj/argoexec:latest')" ]; then
# 	  echo "BANNED image in $f"
# 	  exitcode=1
#   fi
# done
# exit $exitcode
#trap 'kubectl get wf' EXIT

# for f in "${testworkflows[@]}"; do
#   echo "Running $f..."
#   name=$(kubectl create -f "$f" -o name)

#   echo "Waiting for completion of $f..."
#   kubectl wait --timeout=300s --for=condition=Completed "$name"
#   phase="$(kubectl get "$name" -o 'jsonpath={.status.phase}')"
#   echo " -> $phase"
#   if [ Succeeded != "$phase" ]; then
# 	  echo "Test $name in phase $phase"
# 	  exitcode=1
#   fi

#   echo "Deleting $f..."
#   kubectl delete "$name"
# done
printf "%s\0" "${testworkflows[@]}" | xargs -0 -I {} -P $parallel hack/test-one-example.sh {}

exit $exitcode

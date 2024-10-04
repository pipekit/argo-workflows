#!/usr/bin/env bash
set -eu -o pipefail

# Load the configmaps that contains the parameter values used for certain examples.
kubectl apply -f examples/configmaps/simple-parameters-configmap.yaml
# testworkflows=("start")
# #grep -lR 'workflows.argoproj.io/test' examples/* | while read f ; do
# grep -vlR 'workflows.argoproj.io/no-test' examples/*.yaml | while read -r f; do
# 	echo $f
# 	testworkflows+=("$f")
# 	echo "${testworkflows[@]}"
# done


testworkflows=($(grep -vlR 'workflows.argoproj.io/no-test' examples/*.yaml | tr '\n' ' '))
# If arguments, test those files instead
if [ $# -gt 0 ]; then
   testworkflows=("$@")
fi

echo "Checking for banned images..."
for f in "${testworkflows[@]}"; do
  echo " - $f"
  test 0 == "$(grep -o 'image: .*' "$f" | grep -cv 'argoproj/argosay:v2\|python:alpine3.6\|busybox')"
done

#trap 'kubectl get wf' EXIT

for f in "${testworkflows[@]}"; do
  echo "Running $f..."
  name=$(kubectl create -f "$f" -o name)

  echo "Waiting for completion of $f..."
  kubectl wait --for=condition=Completed "$name"
  phase="$(kubectl get "$name" -o 'jsonpath={.status.phase}')"
  echo " -> $phase"
  test Succeeded == "$phase"

  echo "Deleting $f..."
  kubectl delete "$name"
done

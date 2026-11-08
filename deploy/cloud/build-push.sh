#!/usr/bin/env bash
# Build six linux/amd64 images and push them to an existing private repository.
set -euo pipefail

repo="${1:?usage: CONFIRM_PUSH=yes build-push.sh <dockerhub-repo> <image-tag>}"
tag="${2:?usage: CONFIRM_PUSH=yes build-push.sh <dockerhub-repo> <image-tag>}"
cd "$(dirname "$0")/../.."

images=(order-service inventory-service payment-service orchestrator-service gateway-simulator loadgen)
if [ "${CONFIRM_PUSH:-}" != "yes" ]; then
  echo "Dry run; would build and push six images to $repo:" >&2
  for image in "${images[@]}"; do echo "  $repo:$image-$tag" >&2; done
  echo "Create the repository as private, then re-run with CONFIRM_PUSH=yes." >&2
  exit 1
fi

for image in "${images[@]}"; do
  context="$image"
  [ "$image" = "loadgen" ] && context="tools"
  target="$repo:$image-$tag"
  echo "== building $target"
  docker build --platform linux/amd64 -t "$target" "./$context"
  echo "== pushing $target"
  docker push "$target"
done
echo "pushed ${#images[@]} images to $repo with tag $tag"

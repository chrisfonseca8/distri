#!/usr/bin/env bash
# Builds the three service images and the load-test runner image for linux/amd64
# and pushes them to ONE Docker Hub repository, one tag per image:
#   <repo>:order-service-<tag>   <repo>:inventory-service-<tag>
#   <repo>:payment-service-<tag> <repo>:loadgen-<tag>
#
# Pushing publishes the images to Docker Hub. Create the repository as PRIVATE in
# Docker Hub first (a repository created by a first push may default to public),
# then confirm explicitly:
#
#   CONFIRM_PUSH=yes deploy/cloud/build-push.sh ungoodcoder/distri 2026-10-19
set -euo pipefail

repo="${1:?usage: CONFIRM_PUSH=yes build-push.sh <dockerhub-repo> <image-tag>}"
tag="${2:?usage: CONFIRM_PUSH=yes build-push.sh <dockerhub-repo> <image-tag>}"
cd "$(dirname "$0")/../.."

if [ "${CONFIRM_PUSH:-}" != "yes" ]; then
  echo "Dry run. This would build and push to Docker Hub repository: $repo (tag $tag):" >&2
  for image in order-service inventory-service payment-service loadgen; do
    echo "  $repo:$image-$tag" >&2
  done
  echo "Create $repo as a PRIVATE repository first, then re-run with CONFIRM_PUSH=yes." >&2
  exit 1
fi

for context in order-service inventory-service payment-service loadgen; do
  image="$repo:$context-$tag"
  dir="$context"
  [ "$context" = loadgen ] && dir=testing  # image is named loadgen; its source lives in testing/
  echo "== building $image"
  docker build --platform linux/amd64 -t "$image" "./$dir"
  echo "== pushing $image"
  docker push "$image"
done
echo "pushed 4 images to $repo with tag $tag"

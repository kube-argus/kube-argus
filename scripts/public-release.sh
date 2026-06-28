#!/usr/bin/env bash
# scripts/public-release.sh
set -euo pipefail

SOURCE_BRANCH="${1:-main}"
PUBLIC_BRANCH="kube-argus/kube-argus/main"
PUBLIC_AUTHOR="OSS Release <release@kargus.io>"
MESSAGE="${2:-Release $(date +%Y-%m-%d)}"

# Working tree must be clean
git diff-index --quiet HEAD -- || { echo "Working tree dirty"; exit 1; }

if git show-ref --verify --quiet "refs/heads/$PUBLIC_BRANCH"; then
    # --- Branch exists: replace its tree with a new release snapshot ---
    git checkout "$PUBLIC_BRANCH"
    git read-tree "$SOURCE_BRANCH"      # load source tree into index (incl. deletions)
    git checkout-index -a -f            # sync working tree to index
    git commit --author="$PUBLIC_AUTHOR" -m "$MESSAGE" || echo "No changes to release"
else
    # --- First run: create orphan branch, squashing ALL history into one commit ---
    git checkout --orphan "$PUBLIC_BRANCH" "$SOURCE_BRANCH"
    # orphan branch has no parent; index already holds SOURCE_BRANCH's tree
    git commit --author="$PUBLIC_AUTHOR" -m "$MESSAGE"
fi

git checkout "$SOURCE_BRANCH"
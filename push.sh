#!/usr/bin/env bash
#
# Push warpseed, and put the gh account back afterwards.
#
# The repo belongs to the ZyraLabs account while this machine's day-to-day
# account is a different one, so a push needs a switch either side of it.
# Claude cannot make that switch, which is the whole reason this is a script
# you run rather than something that just happens.
#
#   ./push.sh                  push the current branch
#   ./push.sh v1.1.11          push the branch, then that tag
#   ./push.sh --dry-run        say what it would do and change nothing
#
# Pushing a v* tag triggers the release workflow, which builds on a Windows
# runner and publishes warpseed.exe. Naming the tag is the confirmation, so
# only pass one when you mean to release.
set -euo pipefail

OWNER_ACCOUNT=ZyraLabs
DRY_RUN=0
TAGS=()

for arg in "$@"; do
	case "$arg" in
	--dry-run) DRY_RUN=1 ;;
	-h | --help)
		sed -n '3,16p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	-*)
		echo "unknown option: $arg" >&2
		exit 2
		;;
	*) TAGS+=("$arg") ;;
	esac
done

cd "$(git rev-parse --show-toplevel)"
BRANCH=$(git rev-parse --abbrev-ref HEAD)

say() { printf '%s\n' "$*"; }
run() {
	if ((DRY_RUN)); then
		say "  would run: $*"
	else
		"$@"
	fi
}

# What is actually going, so a no-op push does not quietly switch accounts
# twice for nothing.
git fetch --quiet origin "$BRANCH" 2>/dev/null || true
COMMITS=$(git log --oneline "origin/$BRANCH..HEAD" 2>/dev/null || true)

if [[ -z "$COMMITS" && ${#TAGS[@]} -eq 0 ]]; then
	say "Nothing to push: origin/$BRANCH already has $(git rev-parse --short HEAD)."
	exit 0
fi

say "Branch:  $BRANCH"
if [[ -n "$COMMITS" ]]; then
	say "Commits:"
	printf '  %s\n' "$COMMITS"
else
	say "Commits: none — origin/$BRANCH is already up to date"
fi
if [[ ${#TAGS[@]} -gt 0 ]]; then
	say "Tags:    ${TAGS[*]}   (a v* tag publishes a release)"
	for tag in "${TAGS[@]}"; do
		if ! git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
			say "No such tag locally: $tag"
			exit 1
		fi
	done
fi

if [[ -n "$(git status --porcelain)" ]]; then
	say ""
	say "Note: the working tree is dirty. Only committed work is pushed."
fi

ORIGINAL=$(gh api user --jq .login 2>/dev/null || true)
if [[ -z "$ORIGINAL" ]]; then
	say "Could not read the active gh account. Is gh logged in?"
	exit 1
fi

# Put the account back whatever happens below, including a failed push or a
# Ctrl-C. Leaving the shell authenticated as the release account is the one
# outcome worth going out of the way to prevent.
restore() {
	local rc=$?
	if [[ "$ORIGINAL" != "$OWNER_ACCOUNT" ]]; then
		if ((DRY_RUN)); then
			say "  would run: gh auth switch --user $ORIGINAL"
		elif gh auth switch --user "$ORIGINAL" >/dev/null 2>&1; then
			say "gh account back to $ORIGINAL"
		else
			say "WARNING: could not switch back. Run: gh auth switch --user $ORIGINAL"
		fi
	fi
	exit "$rc"
}
trap restore EXIT

say ""
if [[ "$ORIGINAL" == "$OWNER_ACCOUNT" ]]; then
	say "Already on $OWNER_ACCOUNT; no switch needed."
else
	say "Switching gh from $ORIGINAL to $OWNER_ACCOUNT"
	run gh auth switch --user "$OWNER_ACCOUNT"
fi

run git push origin "$BRANCH"
for tag in "${TAGS[@]}"; do
	say "Pushing tag $tag — this starts the release build"
	run git push origin "$tag"
done

if ((DRY_RUN)); then
	say ""
	say "Dry run: nothing was pushed and no account was switched."
	exit 0
fi

# The commit that actually landed, for the same reason update.ps1 prints one:
# a stale remote looks exactly like a broken feature.
say ""
REMOTE_HEAD=$(git ls-remote origin "refs/heads/$BRANCH" | cut -c1-7)
say "origin/$BRANCH is now at $REMOTE_HEAD"
if [[ "$REMOTE_HEAD" == "$(git rev-parse --short=7 HEAD)" ]]; then
	say "Matches local HEAD. Build with update.ps1 and check it prints $REMOTE_HEAD."
else
	say "WARNING: that is NOT local HEAD ($(git rev-parse --short=7 HEAD)). The push did not land."
	exit 1
fi

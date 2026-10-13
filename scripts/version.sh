#!/bin/sh
# Version strings, from the v* release tags (vMAJOR.MINOR.PATCH).
#
#   version.sh                   this checkout: 0.3.1 on a release tag,
#                                0.3.1-4-gabc1234[-dirty] after it, and
#                                0.0.0-N-gHASH before the first release
#   version.sh next patch|minor|major
#                                the tag the next release gets, e.g. v0.3.2
set -eu

latest() {
	git tag --list 'v[0-9]*.[0-9]*.[0-9]*' --merged HEAD --sort=-v:refname | head -n1
}

case "${1:-}" in
'')
	if [ -n "$(latest)" ]; then
		git describe --tags --match 'v[0-9]*.[0-9]*.[0-9]*' --dirty | sed 's/^v//'
	else
		dirty=$(git diff --quiet HEAD 2>/dev/null || echo -dirty)
		echo "0.0.0-$(git rev-list --count HEAD)-g$(git rev-parse --short HEAD)$dirty"
	fi
	;;
next)
	cur=$(latest | sed 's/^v//')
	IFS=. read -r major minor patch <<-EOF
		${cur:-0.0.0}
	EOF
	case "${2:-}" in
	major) echo "v$((major + 1)).0.0" ;;
	minor) echo "v$major.$((minor + 1)).0" ;;
	patch) echo "v$major.$minor.$((patch + 1))" ;;
	*) echo "usage: $0 next patch|minor|major" >&2; exit 2 ;;
	esac
	;;
*)
	echo "usage: $0 [next patch|minor|major]" >&2; exit 2
	;;
esac

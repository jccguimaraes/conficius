#!/bin/sh

if [ -n "${NO_COLOR:-}" ] || ! [ -t 1 ]; then
    BLUE=""
    RESET=""
else
    BLUE="$(printf '\033[1;34m')"
    RESET="$(printf '\033[0m')"
fi

heading() {
    printf '%s%s%s\n' "$BLUE" "$1" "$RESET"
}

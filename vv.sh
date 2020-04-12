#!/bin/bash

HOST=https://vetle.vodka
CMD="$1"

function help () {
    echo "$0 commands:"
    echo "  s: share a file"
    echo "    vv s <filename> [shotKey]"
    echo "  f: fetch a file"
    echo "    vv f <shotKey>"
}

function argrequired () {
    if [ -z $2 ]; then
        echo "Argument '$1' is missing"
        exit 1
    fi
}

if [ "$CMD" == "s" ]; then
    argrequired filename $2
    shotKey=$3
    curl -F file=@$2 $HOST/$shotKey
elif [ "$CMD" == "f" ]; then
    argrequired shotKey $2
    curl $HOST/$2
elif [ "$CMD" == "l" ]; then
# Link, pretty much a simple URL shortener
    argrequired shotKey $2
    argrequired link $3
    contents="<html><meta http-equiv='refresh' content='0; url=$3'></html>"
    curl --form-string text="$contents" $HOST/$2
else
    help
fi

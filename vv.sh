#!/bin/bash

HOST=https://vetle.vodka
CMD="$1"

function help () {
    echo "$0 commands:"
    echo "  a: share a file as admin (password prompt)"
    echo "    vv a <filename> [shotKey] [numdls]"
    echo "  s: share a file"
    echo "    vv s <filename> [shotKey] [numdls]"
    echo "  f: fetch a file"
    echo "    vv f <shotKey>"
}

function argrequired () {
    if [ -z $2 ]; then
        echo "Argument '$1' is missing"
        exit 1
    fi
}

case "$CMD" in 
    # Admin - Prompt for admin password, then run share
    a)
        printf "Admin key: "
        read -s adminKey
        echo
        ;&
    s)
        argrequired filename $2
        shotKey=$3
        numdls=$4
        curlArgs="-H 'Content-Type: multipart/form-data' -F file=@$2"
        if [ -n "$numdls" ]; then
            curlArgs="$curlArgs -F 'numdls=$numdls'"
        fi
        if [ -n "$adminKey" ]; then
            curlArgs="$curlArgs -H 'Admin-Key: $adminKey'"
        fi
        echo curl "$curlArgs" $HOST/$shotKey | bash
        ;;
    # Fetch - Get data
    f)
        argrequired shotKey $2
        curl $HOST/$2
        ;;
    # Link, pretty much a simple URL shortener
    l)
        argrequired shotKey $2
        argrequired link $3
        contents="<html><meta http-equiv='refresh' content='0; url=$3'></html>"
        curl --form-string text="$contents" $HOST/$2
        ;;
    *)
        help
esac

#!/usr/bin/env bash

# Install a version of the operator sdk.
# https://sdk.operatorframework.io/docs/installation/#install-from-github-release

set -euo pipefail

case $(uname -m) in x86_64|aarch64) ARCH="amd64";; *) ARCH="$(uname -m)";; esac
OS=$(uname | awk '{print tolower($0)}')
OPERATOR_SDK_DL_URL=https://github.com/operator-framework/operator-sdk/releases/download/${2}
OPERATOR_SDK_DL_BINARY=${OPERATOR_SDK_DL_URL}/operator-sdk_${OS}_${ARCH}

if [ ! -f "$1" ]; then
  TMP_DIR=$(mktemp -d)
  cd $TMP_DIR

  # GPG can hit "no route for host" if using IPv6 on mac (https://github.com/rvm/rvm/issues/4215#issuecomment-486609350)
  if [[ $OS == 'darwin' ]]; then
    mkdir -p "/Users/runner/.gnupg"
    echo "Disabling ipv6 for gpg on mac"
    echo "disable-ipv6" >  /Users/runner/.gnupg/dirmngr.conf
    gpgconf --kill all
  fi

  echo "Downloading $OPERATOR_SDK_DL_BINARY"
  curl -sLO $OPERATOR_SDK_DL_BINARY
  gpg --keyserver keyserver.ubuntu.com --recv-keys 052996E2A20B5C7E
  curl -sLO ${OPERATOR_SDK_DL_URL}/checksums.txt
  curl -sLO ${OPERATOR_SDK_DL_URL}/checksums.txt.asc
  gpg -u "Operator SDK (release) <cncf-operator-sdk@cncf.io>" --verify checksums.txt.asc
  if [[ $OS == 'darwin' ]]; then
    grep operator-sdk_${OS}_${ARCH} checksums.txt | shasum -a 256 -c -
  else
    grep operator-sdk_${OS}_${ARCH} checksums.txt | sha256sum -c -
  fi
  mkdir -p "$(dirname $1)"
  chmod +x operator-sdk_${OS}_${ARCH} && mv operator-sdk_${OS}_${ARCH} $1

  rm -rf $TMP_DIR
fi

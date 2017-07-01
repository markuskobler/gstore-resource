#!/bin/bash
set -eu

export GOPATH=${PWD}/go
export PATH=${GOPATH}/bin:${PATH}

src=${GOPATH}/src/github.com/markuskobler/gstore-resource
out=${PWD}/build

cd ${src}

tag=$(git describe --exact-match --abbrev=0 || true)
tag=${tag:-dev}
commit=$(git rev-parse --short HEAD)

_ca_certificates() {
  echo ">>> copy /etc/ssl/certs/ca-certificates.crt"
  certs=("/etc/ssl/certs/ca-certificates.crt" "/etc/pki/tls/certs/ca-bundle.crt" "/etc/ssl/ca-bundle.pem")
  for cert in "${certs[@]}"; do
      if [ -f ${cert} ]; then
          mkdir -p ${out}/etc/ssl/certs
          cat ${cert} > ${out}/etc/ssl/certs/ca-certificates.crt
          break
      fi
  done
}

_build() {

    export CGO_ENABLED=0

    echo ">>> Build gstore-resource ${tag} (${commit})"
    go build \
       -ldflags "-X main.tag=${tag} -X main.commit=${commit}" \
       -o ${out}/gstore-resource .

    echo ${tag} > ${out}/tag

    mkdir -p ${out}/etc

  cat <<EOF > ${out}/etc/passwd
root:x:0:0:root:/:/dev/null
nobody:x:65534:65534:nogroup:/:/dev/null
EOF

  cat <<EOF > ${out}/etc/group
root:x:0:
nogroup:x:65534:
EOF

    cp -r ${src}/Dockerfile ${src}/resource ${out}
}

_ca_certificates
_build

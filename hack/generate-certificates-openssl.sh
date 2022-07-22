#!/bin/bash

# used to generate certificates for webhooks in integration tests.
# usage: generate-certificates-openssl [cert_dir] [webhook_server_ip]

cert_dir=$1
webhook_ip=$2

if [[ ${cert_dir} == "" ]]; then
  cert_dir="/tmp/gateway-operator-webhook-certs"
fi

if [[ ${webhook_ip} == "" ]]; then
  webhook_ip=$(ip addr | grep "inet " | grep "global" | head -n 1 | awk '{print $2}' | awk -F'/' '{print $1}')
fi

echo "generate certificates for IP ${webhook_ip}"

mkdir -p ${cert_dir}
cert_dir=$(cd ${cert_dir} && pwd)
echo "create self signed certificates for gateway operator webhook server in ${cert_dir}"

echo "[ alt_names ]
IP.1=${webhook_ip}

[ v3_ext ]
subjectAltName=@alt_names" > ${cert_dir}/webhook_ext.cnf

openssl genrsa -out ${cert_dir}/ca.key 2048
openssl req -x509 -new -nodes -key ${cert_dir}/ca.key -out  ${cert_dir}/ca.crt\
 -subj "/CN=gateway-operator-webhook.kong-system"
openssl genrsa -out ${cert_dir}/tls.key 2048
openssl req -new -key  ${cert_dir}/tls.key -out ${cert_dir}/webhook.csr\
 -subj "/C=US/ST=California/L=San Francisco/O=Kong/OU=Org/CN=gateway-operator-webhook.kong-system"
openssl x509 -req -in ${cert_dir}/webhook.csr -CA ${cert_dir}/ca.crt -CAkey ${cert_dir}/ca.key \
 -CAcreateserial -out ${cert_dir}/tls.crt -extensions v3_ext -extfile ${cert_dir}/webhook_ext.cnf

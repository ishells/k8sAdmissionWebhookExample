#!/usr/bin/env sh
set +e

caBundle=$(cat tls.crt | base64 -w 0)

kubectl patch validatingwebhookconfiguration validating-application-standards --type='json' -p "[{'op': 'add', 'path': '/webhooks/0/clientConfig/caBundle', 'value':'${caBundle}'}]"

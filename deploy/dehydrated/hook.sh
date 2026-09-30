#!/bin/sh

# dehydrated's hook on the origin, installed as /etc/dehydrated/hook.sh.
#
# DNS-01, for the certificates that need it: the share wildcards,
# share.openipc.org and share.openipc.cloud (a wildcard cannot be issued over
# HTTP-01). Their per-certificate configs, /var/lib/dehydrated/certs/<name>/config,
# select dns-01; every other certificate stays on HTTP-01 and never reaches
# these lines.
case "$1:$2" in
  deploy_challenge:share.openipc.org|deploy_challenge:\*.share.openipc.org|\
  deploy_challenge:share.openipc.cloud|deploy_challenge:\*.share.openipc.cloud)
    exec /etc/dehydrated/hetzner-dns01.py add "$2" "$4" ;;
  clean_challenge:share.openipc.org|clean_challenge:\*.share.openipc.org|\
  clean_challenge:share.openipc.cloud|clean_challenge:\*.share.openipc.cloud)
    exec /etc/dehydrated/hetzner-dns01.py remove "$2" "$4" ;;
esac

test "$1" = "deploy_cert" || exit 0

# share.openipc.cloud is served by the openipc.kz host, which keeps no DNS
# token (deploy/nginx/mirrors/kz/install-tls.sh says why), so the wildcard is
# issued here and handed over. The key can only run the installer there
# (deploy/nginx/mirrors/kz/share-cert/). A failed hand-over is loud but does
# not stop this host's own reload: the old certificate there has weeks left.
# deploy_cert's arguments: $3 the key, $5 the full chain -- the privkey.pem
# and fullchain.pem links in the certificate's directory, which tar -h follows.
if [ "$2" = share.openipc.cloud ]; then
  tar -C "$(dirname "$5")" -chf - "$(basename "$3")" "$(basename "$5")" |
    ssh -i /root/.ssh/share-cert-push -o BatchMode=yes ubuntu@194.238.42.216 install-share-cert \
    || echo "hook: share.openipc.cloud was renewed but NOT installed on openipc.kz" >&2
fi

# Restart Nginx
nginx -s reload

# Notify via telegram
#/opt/sbin/message_telegram dehydrated

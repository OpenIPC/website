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

# share.openipc.cloud is served by the openipc.kz host, which keeps no DNS
# token (deploy/nginx/mirrors/kz/install-tls.sh says why), so the wildcard is
# issued here and handed over. The key can only run the installer there
# (deploy/nginx/mirrors/kz/share-cert/), which does nothing for a pair it
# already has. It is sent on unchanged_cert too -- every nightly run -- so a
# hand-over that failed is retried the next night rather than at the next
# renewal. deploy_cert's and unchanged_cert's arguments: $3 the key, $5 the
# full chain, the privkey.pem and fullchain.pem links tar -h follows.
case "$1" in
  deploy_cert|unchanged_cert) ;;
  *) exit 0 ;;
esac
# A failure is reported, not returned: a hook that fails can stop dehydrated
# before it reaches the certificates after this one.
if [ "$2" = share.openipc.cloud ]; then
  tar -C "$(dirname "$5")" -chf - "$(basename "$3")" "$(basename "$5")" |
    ssh -i /root/.ssh/share-cert-push -o BatchMode=yes ubuntu@194.238.42.216 install-share-cert ||
    echo "hook: share.openipc.cloud NOT installed on openipc.kz; retried next run" >&2
fi
test "$1" = "deploy_cert" || exit 0

# Restart Nginx
nginx -s reload

# Notify via telegram
#/opt/sbin/message_telegram dehydrated

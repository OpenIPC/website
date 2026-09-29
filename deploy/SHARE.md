# Camera sharing links: bringing up share.openipc.org

The `share` role (`openipc serve --role share`, :3004 prod / :3014 dev) is the
signalling relay and the page behind links like
`https://<id>.share.openipc.org/#<secret>`. `deploy.sh` deploys it with the
other two roles; what it cannot do on its own is give it a name. In order:

1. **DNS.** `share.openipc.org` and `*.share.openipc.org` → this host
   (A/AAAA, same addresses as openipc.org). Cameras dial
   `wss://share.openipc.org/__share/device`; guests open `<id>.share.openipc.org`.

2. **Certificate.** One wildcard, `share.openipc.org` + `*.share.openipc.org`,
   in `/var/lib/dehydrated/certs/share.openipc.org/`. HTTP-01 cannot issue a
   wildcard, so this one certificate uses DNS-01 and the others keep HTTP-01:
   - `/etc/dehydrated/domains.txt` lists `share.openipc.org *.share.openipc.org`;
   - `/var/lib/dehydrated/certs/share.openipc.org/config` sets
     `CHALLENGETYPE="dns-01"` for it alone;
   - `/etc/dehydrated/hook.sh` hands `deploy_challenge`/`clean_challenge` for
     those two names to `/etc/dehydrated/hetzner-dns01.py`, which adds the
     `_acme-challenge.share` TXT value through the Hetzner Cloud API, waits for
     every authoritative nameserver to serve it, and removes it afterwards;
   - the API token is `/etc/dehydrated/hetzner-dns.token` (0600, root).

   The nightly `dehydrated -c -g` renews it with the rest. The chain must end
   in ISRG Root X1 or X2: cameras pin those two and nothing else.

3. **The vhost**, `deploy/nginx/sites-available/org.openipc.share`, goes out
   with `deploy/push-nginx.sh --apply` like every other. It names the
   certificate above, so it only belongs on a host that has one.

4. **Deploy** as usual (`openipc-deploy prod <sha>`); it now starts
   `go-share-prod` and waits for `:3004/up`.

5. **Check.**
   `curl -s https://share.openipc.org/__share/ice` answers the ICE list;
   `curl -si https://share.openipc.org/__share/device` answers 426 (a WebSocket
   is expected). Then share a camera from its WebUI and open the link from
   another network.

## Settings

In `/srv/www/.env.go-<env>`, all optional:

| Variable | Default | |
|---|---|---|
| `SHARE_ORIGINS` | `*.share.openipc.org` | Hosts a page's signalling socket may come from. |
| `SHARE_STUN_URLS` | `stun:stun.cloudflare.com:3478` | Given to the page. |
| `SHARE_TURN_URLS` | none | TURN for guests behind networks that block UDP. |
| `SHARE_TURN_SECRET` | none | coturn's `static-auth-secret`; credentials are minted per page (TURN REST). |

Without TURN a guest whose network blocks UDP entirely cannot connect;
everything else goes peer to peer. The camera itself needs no TURN: it reaches
whatever relay the page allocates with ordinary outbound UDP.

## What the relay holds

Nothing that outlives a socket, and nothing secret: which camera socket
registered which share id, until the share expires or the socket closes. The
share's secret never reaches it -- the page and the camera prove it to each
other over their own DTLS connection. A restart costs every camera one
reconnect (1 s, backing off to 2 min).

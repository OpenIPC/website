# Camera sharing links: share.openipc.cloud

The `share` role (`openipc serve --role share`, :3004 prod / :3014 dev) is the
signalling relay and the page behind links like
`https://<id>.share.openipc.cloud/#<secret>`. Cameras dial
`wss://share.openipc.cloud/__share/device`.

**Why these names, and why they point at the openipc.kz host.** Some
providers -- Russia's among them -- filter openipc.org's addresses, so a
guest behind one could not open a link served from here, and a camera behind
one could not register a share. The openipc.kz host (194.238.42.216) is
reachable from both sides, so the share names point there, and its nginx
passes everything on to the one share role here. One role, because it keeps
its registry in memory: a second copy on the openipc.kz host would split
cameras from their guests. Only signalling takes this path -- a session's
traffic goes peer to peer, or through a TURN relay (below).

`deploy.sh` deploys the role with the other two; what it cannot do on its own
is give it a name. In order:

1. **DNS.** `share.openipc.cloud` and `*.share.openipc.cloud`, A records to
   194.238.42.216, in the Hetzner zone `openipc.cloud`.

2. **Certificate.** One wildcard, `share.openipc.cloud` + `*.share.openipc.cloud`,
   issued here, in `/var/lib/dehydrated/certs/share.openipc.cloud/`. HTTP-01
   cannot issue a wildcard, so this one certificate uses DNS-01 and the others
   keep HTTP-01:
   - `/etc/dehydrated/domains.txt` lists `share.openipc.cloud *.share.openipc.cloud`;
   - `/var/lib/dehydrated/certs/share.openipc.cloud/config` sets
     `CHALLENGETYPE="dns-01"` for it alone;
   - `/etc/dehydrated/hook.sh` (`deploy/dehydrated/`) hands
     `deploy_challenge`/`clean_challenge` for those two names to
     `/etc/dehydrated/hetzner-dns01.py`, which adds the `_acme-challenge.share`
     TXT value through the Hetzner Cloud API, waits for every authoritative
     nameserver to serve it, and removes it afterwards;
   - the API token is `/etc/dehydrated/hetzner-dns.token` (0600, root).

   The nightly `dehydrated -c -g` renews it with the rest. The chain must end
   in ISRG Root X1 or X2: cameras pin those two and nothing else.

   The openipc.kz host needs the same certificate and keeps no DNS token by
   design (its `install-tls.sh` says why), so `hook.sh` hands the pair over on
   every run -- `deploy_cert` and the nightly `unchanged_cert` alike, so a
   failed hand-over is retried the next night -- as a tar over ssh with
   `/root/.ssh/share-cert-push`. Its authorized_keys entry there is
   `restrict,command="sudo -n /usr/local/sbin/install-share-cert"`, and that
   installer (`mirrors/kz/share-cert/`) accepts exactly two regular files,
   checks that they are a pair naming the wildcard and chaining to a trusted
   root, leaves a pair it already has alone, and swaps both files together,
   restoring the previous pair if nginx will not take the new one.

3. **The vhosts.** Here, `deploy/nginx/sites-available/cloud.openipc.share`,
   with `deploy/push-nginx.sh --apply`: it serves the share names with the
   wildcard and closes every connection that does not come from the
   openipc.kz host. There,
   `deploy/nginx/mirrors/kz/sites-available/cloud.openipc.share`, with that
   directory's `push.sh --apply`: it proxies to this host by address,
   checking this certificate's name, with the reader's Host (the share role
   reads the share id from it) and X-Forwarded-For (which nginx.conf here
   trusts from that host). Both name the certificate, so it goes first.

4. **Deploy** as usual (`openipc-deploy prod <sha>`); it starts
   `go-share-prod` and waits for `:3004/up`.

5. **Check.**
   `curl -s https://share.openipc.cloud/__share/ice` answers the ICE list (STUN
   only: TURN is for a live share's own host);
   `curl -si https://share.openipc.cloud/__share/device` answers 426 (a
   WebSocket is expected). Then share a camera from its WebUI and open the
   link from another network.

## Settings

In `/srv/www/.env.go-<env>`, all optional:

| Variable | Default | |
|---|---|---|
| `SHARE_ORIGINS` | `*.share.openipc.cloud` | Hosts a page's signalling socket may come from. |
| `SHARE_STUN_URLS` | `stun:stun.cloudflare.com:3478` | Given to the page. Prod: `stun:openipc.kz:3478` first -- Cloudflare is throttled in places. |
| `SHARE_TURN_URLS` | none | TURN for guests whose network and the camera's cannot meet directly. |
| `SHARE_TURN_SECRET` | none | coturn's `static-auth-secret`; credentials are minted per page (TURN REST). |

Without TURN a guest whose network blocks UDP, or two ends that both sit
behind address-per-destination NATs (mobile carriers, some offices), cannot
connect; everything else goes peer to peer. The camera itself needs no TURN:
it reaches whatever relay the page allocates with ordinary outbound UDP.

## TURN

coturn runs on the three hosts behind openipc.org, openipc.kz and openipc.ru,
each on UDP and TCP 3478 with relay ports 49160-49999, all holding the same
secret:

```
SHARE_TURN_URLS=turn:openipc.kz:3478?transport=udp,turn:openipc.ru:3478?transport=udp,turn:openipc.org:3478?transport=udp,turn:openipc.kz:3478?transport=tcp,turn:openipc.ru:3478?transport=tcp,turn:openipc.org:3478?transport=tcp
```

The page is given every URL and ICE keeps whichever relay works -- Chrome
prefers them in the order given, so openipc.kz, reachable from everywhere,
comes first. A direct path, when there is one, still wins over all of them.

**Credentials.** `/__share/ice` issues a TURN credential only to a page that
sends the share's relay token (`X-Share-Token`, derived from the link's
secret; the share id alone is in the host name and proves nothing) for a
share a camera is serving right now, and it expires five minutes later, named
`<expiry>:<share id>` so coturn's log says whose relay it was. coturn checks
it when the page allocates; an allocation it granted lives on for as long as
the page refreshes it, so a session outlasts its credential. A credential
copied out of a page is worth minutes of relay, not the link's lifetime.

**What a relay may reach.** Only the Internet: `deploy/turn/turnserver.conf`
denies every private, loopback, link-local and multicast range, and the host's
own address, as a peer. TCP relaying is off -- the camera end is always UDP.

**Install or update** with `deploy/turn/install.sh`, the secret in a file:

```
TURN_SECRET_FILE=~/turn.secret deploy/turn/install.sh 37.27.251.71 -p 35242 root@openipc.org
TURN_SECRET_FILE=~/turn.secret deploy/turn/install.sh 194.238.42.216 ubuntu@194.238.42.216
TURN_SECRET_FILE=~/turn.secret TURN_DOCKER=1 deploy/turn/install.sh 194.58.109.202 -p 35242 root@194.58.109.202
```

openipc.ru's host is shared with other sites, so coturn runs there as the
upstream image (`coturn-share`, host network) and leaves its packages alone.
Rotating the secret means all three hosts and `SHARE_TURN_SECRET`, then a
restart of the share role. Restarting coturn drops the sessions it is
relaying at that moment; direct ones are untouched.

**Check** a host without a browser, from any other one, with a credential
minted from the secret: `turnutils_uclient -e <public echo peer> -u <user> -w
<pass> <host>` answers with the round trip, and an expired credential must be
refused. In a browser, `#<secret>&relay` on a share link forces the page to use
a relay, and Diagnostics shows which one it took.

## What the relay holds

Nothing that outlives a socket, and nothing secret: which camera socket
registered which share id, until the share expires or the socket closes. The
share's secret never reaches it -- the page and the camera prove it to each
other over their own DTLS connection. A restart costs every camera one
reconnect (1 s, backing off to 2 min).

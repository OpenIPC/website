// A few assertions about the parts of redact.js that decide what counts as
// identifying. Run by run.sh before it touches the camera, because three
// separate defects in this logic have been found by review and all of them
// were invisible in a successful run: the rules had simply stopped covering
// something, and everything still reported clean.
//
// Covers config() and survivors(), which are pure. It does not cover
// redactInPage, which needs a DOM -- that one is exercised by the run itself,
// and by the check shoot.js makes against the page it is about to photograph.
const { config, survivors } = require('./redact');

let failed = 0;
function check(what, got, want) {
  const ok = JSON.stringify(got) === JSON.stringify(want);
  if (!ok) { console.log(`FAIL ${what}\n  got  ${JSON.stringify(got)}\n  want ${JSON.stringify(want)}`); failed++; }
}

// A bare label is not identifying, and rewriting it would eat ordinary words.
check('bare hostname is not rewritten', config({ host: 'camera' }).host, null);
check('bare hostname has no domain', config({ host: 'camera' }).domain, null);

// The stand-in must never be treated as the thing it stands in for, or every
// capture fails as a leak of itself.
check('the stand-in is not rewritten', config({ host: 'camera.local' }).host, null);

// A qualified name and its suffix are both identifying.
const lab = config({ host: 'cam1.lab.example.com', ips: ['10.1.2.3'] });
check('qualified hostname', lab.host, 'cam1.lab.example.com');
check('its domain', lab.domain, 'lab.example.com');

// A single-label suffix is a word a UI may well use.
check('single-label domain is left alone', config({ host: 'cam.lan' }).domain, null);

// An address given instead of a name is an address, not a name.
const byIp = config({ host: '10.1.2.3', ips: ['10.1.2.3'] });
check('numeric host is not a name', byIp.host, null);
check('numeric host is an address', byIp.ips, ['10.1.2.3']);

// Nothing identifying in redacted text.
const clean = 'Camera Preview\ncamera.local 192.168.1.10 gw 192.168.1.1 00:11:22:33:44:55';
check('clean page', survivors(clean, lab), []);
check('clean page, reached by address', survivors(clean, byIp), []);

// ...and everything that is.
check('the hostname', survivors('cam1.lab.example.com', lab), ['hostname cam1.lab.example.com']);
check('the domain alone', survivors('search lab.example.com', lab), ['domain lab.example.com']);
check('the camera address', survivors('IP 10.1.2.3', lab), ['address 10.1.2.3', 'private address 10.1.2.3']);
check('a peer address', survivors('peer 10.9.9.9', lab), ['private address 10.9.9.9']);
check('a real MAC', survivors('2c:6f:51:aa:bb:cc', lab), ['MAC 2c:6f:51:aa:bb:cc']);
check('a hyphenated MAC', survivors('2c-6f-51-aa-bb-cc', lab), ['MAC 2c-6f-51-aa-bb-cc']);
check('a link-local IPv6', survivors('fe80::1c2d:3e4f', lab), ['IPv6 fe80::1c2d:3e4f']);
check('a unique-local IPv6', survivors('fd12:3456::9', lab), ['IPv6 fd12:3456::9']);

// A name that merely ends with the domain is a different site.
check('a lookalike domain', survivors('notlab.example.com.evil.test', lab), []);

console.log(failed ? `${failed} selftest failure(s)` : 'selftest: redaction rules behave');
process.exit(failed ? 1 : 0);

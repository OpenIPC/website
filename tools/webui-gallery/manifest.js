// The filenames config/webui_gallery.yml implies, one per line.
//
// run.sh deletes anything in the images directory that this does not list, and
// it used to work that list out with grep -- a second, worse YAML parser living
// in the shell. `slug: "status"` is perfectly good YAML and both real readers
// accept it, but the grep did not, so a screen written that way would have had
// its freshly installed images deleted as orphans moments after being made.
const fs = require('fs');
const yaml = require('js-yaml');

const screens = yaml.load(fs.readFileSync(process.env.MANIFEST, 'utf8')).screens;
for (const { slug } of screens) console.log(`${slug}.webp\n${slug}-thumb.webp`);

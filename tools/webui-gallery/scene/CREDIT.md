# Where the scene comes from

`beach-usa.jpg` is a snapshot from a camera belonging to **@usa-**, an OpenIPC
user, posted at
<https://github.com/OpenIPC/majestic/issues/300#issuecomment-5405996706> while
reporting a saturation bug on SSC337DE, and reused here with their permission
(same thread, "Sure :)").

It is 2400x1440 straight off the camera's `image.jpg`, with no OSD. The gallery
crops it to the player's aspect at capture time; nothing is edited into it.

It replaces the picture in the two pages that show a live player, because the
lab camera the gallery is shot from faces an unlit bench of cables that
photographs as a dark smear.

## If you change it

Two things have to move with the file:

1. **The right to publish it.** A camera's view is somebody's property and often
   somebody's home. Use a picture the project has been given, and record the
   permission here as above.
2. **The credit on the page**, which names @usa- by hand:
   `pages.web_interface.scene_credit_html` in `config/locales/pages.*.yml`, in
   all ten locales. `test/controllers/web_interface_test.rb` asserts the credit
   and its link are present, so a swap that forgets this fails the suite --
   but the test cannot tell whether the name is the *right* one.

If you photograph a camera whose view needs no substitution, pass
`--scene none` and drop the credit paragraph from the view.

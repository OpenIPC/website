# Where the scene comes from

`beach-usa.jpg` is a snapshot from a camera belonging to **@usa-**, an OpenIPC
user, posted at
<https://github.com/OpenIPC/majestic/issues/300#issuecomment-5405996706> while
reporting a saturation bug on SSC337DE, and reused here with their permission
(same thread, "Sure :)").

It is 2400x1440 straight off the camera's `image.jpg`, with no OSD. The gallery
crops it to the player's aspect at capture time; nothing is edited into it.

It replaces the picture on every page that shows the camera's own -- the
players, the dashboard's snapshot tile, the recordings player -- because the
lab camera the gallery is shot from faces an unlit bench of cables that
photographs as a dark smear.

## If you change it

Two things have to move with the file:

1. **The right to publish it.** A camera's view is somebody's property and often
   somebody's home. Use a picture the project has been given, and record the
   permission here as above.
2. **The credit on the page**, which names @usa- by hand:
   `pages.web_interface.scene_credit_html` in `data/locales/pages.*.yml`, in
   all ten locales. the site's build tests assert the credit
   and its link are present, so a swap that forgets this fails the suite --
   but the test cannot tell whether the name is the *right* one.

If you photograph a camera whose view needs no substitution, pass
`--scene none` and drop the credit paragraph from the view.

## The street: street.jpg and street.json

The street on the staged motion detection screen is frame 120 of the recording
behind the tracking video on /web-interface (`frontend/apps/site/public/video`),
a high-angle view of a public street. The boxes burned into that recording were
inpainted out, and every person's head is pixelated; nothing else is edited.
`street.json` holds the three people's boxes in that frame, from the reference
tracks the video was made with.

Both the picture and the video show passers-by. If either is ever replaced,
pixelate every face in every frame before it is committed, and look at the
frames: the tracks that place the pixelation lose people behind trees exactly
where their faces still show.

# frozen_string_literal: true

# The markup for a camera frame, in one place.
#
# Every wall surface used to emit `image_tag snapshot.wall_image(:variant)`,
# which put a fetchable URL for somebody's premises into six different
# templates. A canvas carries only the id and the variant; the bytes arrive
# over WallChannel and are painted by src/wall.js. There is no address here
# that returns an image.
module WallHelper
  # A canvas sized to the variant it will hold, so the page does not reflow
  # when frames land. These are the `resize_to_limit` bounds declared on
  # Snapshot's attachment, and the canvas keeps the same letterboxing the
  # `h-100 w-auto` img classes used to give: fitted by height, centred, with
  # the container's background showing at the sides.
  FRAME_SIZES = {
    'icon' => [90, 60],
    'icon2' => [240, 135],
    'thumb' => [480, 360],
    'fullhd' => [1920, 1080]
  }.freeze

  def wall_frame_tag(snapshot, variant, css_class: nil, alt: nil, **options)
    width, height = FRAME_SIZES.fetch(variant.to_s)

    tag.canvas('',
               width: width, height: height, class: css_class, role: 'img',
               'aria-label': alt || t('snapshots.icon.snapshot_alt'),
               data: { wall_frame: snapshot.public_id, wall_variant: variant },
               **options)
  end

  # Said once per wall page, in place of the image fallback that used to be
  # here.
  #
  # #261 put a plain link beside the scripted path so readers without
  # JavaScript could still reach every frame, and the crawler followed it --
  # distinct ids harvested went UP, 2,255 to 3,188. So there is no fallback
  # now, on purpose, and the page says why rather than looking broken.
  def wall_noscript
    safe_join([tag.noscript(t('snapshots.index.needs_javascript'), class: 'alert alert-info'),
               wall_status_slot])
  end

  # Where src/wall.js says why there are no pictures.
  #
  # Empty and hidden until something goes wrong. Two things can: the channel
  # refuses a client that has taken its hourly budget, and -- the case this was
  # really added for -- the socket never opens at all, which is what a reader
  # behind a mirror whose nginx does not forward the Upgrade experiences.
  # openipc.kz and openipc.cloud are in that state today. Without this they
  # would see a grid of blank squares indistinguishable from an empty wall.
  def wall_status_slot
    status = { wall_status: t('snapshots.index.frames_unavailable') }

    tag.p('', class: 'alert alert-warning', hidden: true, data: status)
  end
end

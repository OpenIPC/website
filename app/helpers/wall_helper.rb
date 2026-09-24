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

    # Every frame this page renders is one the channel may later be asked for,
    # and this is the only moment the server knows the full set. Collected here
    # rather than recomputed in the controller so that a surface cannot be
    # added later that draws frames without authorising them -- there are seven
    # of them and they do not share an action.
    wall_granted_pairs << WallGrant.pair(snapshot.public_id, variant)

    tag.canvas('',
               width: width, height: height, class: css_class, role: 'img',
               'aria-label': alt || t('snapshots.icon.snapshot_alt'),
               data: { wall_frame: snapshot.public_id, wall_variant: variant },
               **options)
  end

  # The grant for every frame rendered above it.
  #
  # It has to come last: the ids are not known until the body has rendered,
  # which is why this cannot sit in <head> beside action_cable_meta_tag. The
  # layout calls it after `yield`, and it produces nothing at all on a page
  # that drew no frames -- which is most of the site.
  #
  # It is ALSO called inside the two lazy turbo-frames, and that is not
  # belt-and-braces. Turbo extracts the matching <turbo-frame> from the
  # response and throws the rest away, so a grant emitted by the layout never
  # reaches the document when the frame is what was fetched -- the reader would
  # be left holding the previous page's permission, which names none of the 96
  # frames that just arrived. A grant inside the frame travels with it.
  #
  # Emitted as a data attribute rather than a <meta> because this appears in
  # the body, where a meta does not belong, and because every other wall hook
  # in this codebase is already data-wall-*.
  def wall_grant_tag
    return if wall_granted_pairs.empty?

    grant = WallGrant.issue(pairs: wall_granted_pairs.to_a)

    # Emptied on the way out, so the layout's unconditional call after `yield`
    # adds nothing on a page whose frames were already authorised from inside a
    # turbo-frame. Without this the archive and the slideshow carry two grant
    # elements, and whichever the client reads first is a coin toss.
    @wall_granted_pairs = Set.new
    return if grant.blank?

    tag.div('', hidden: true, data: { wall_grant: grant })
  end

  def wall_granted_pairs
    @wall_granted_pairs ||= Set.new
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

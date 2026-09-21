# The static bundle as a container image.
#
# The registry is the one artifact store webber-eu already talks to, anonymously
# and every day, and the tag is the 40-hex commit -- the same addressing
# openipc-deploy rolls an image back by. That is the whole reason this is an
# image rather than a tarball: a GitHub Actions artifact expires, needs a token
# to download, and is addressed by run id rather than by commit, which makes it
# a poor thing to reach for during a rollback.
#
# It is data, not a program, and nothing can run it.
#
# THE BUILD CONTEXT IS dist/, NOT THE REPOSITORY ROOT:
#
#   deploy/static/build.sh dist
#   docker build -f deploy/static/Dockerfile dist
#
# A root context would send the whole repository for one COPY, and would leave
# the bundle at the mercy of a .dockerignore written for the Rails image -- a
# line excluding dist/ there would publish an empty bundle and nothing would
# say so.
FROM scratch
COPY . /bundle/

# Never executed: there is no libc in this image and no kernel could run it.
# It exists because `docker create` refuses an image with neither CMD nor
# ENTRYPOINT, and `docker create` plus `docker cp` is how deploy/static.sh
# reads a bundle out of a scratch image that has no shell.
CMD ["/bundle-is-data-not-a-program"]

LABEL org.opencontainers.image.description="openipc.org static bundle (#157)"
# org.opencontainers.image.revision comes from docker/metadata-action in
# .github/workflows/build.yml. An image built by hand carries none, and
# deploy/static.sh refuses it deliberately: a bundle whose commit is unknown is
# a bundle nothing can roll back to.

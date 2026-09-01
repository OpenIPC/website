# Puma can serve each request in a thread from an internal thread pool.
# The `threads` method setting takes two numbers: a minimum and maximum.
# Any libraries that use thread pools should be configured to match
# the maximum value specified for Puma. Default is set to 5 threads for minimum
# and maximum; this matches the default thread size of Active Record.
#
max_threads_count = ENV.fetch("RAILS_MAX_THREADS") { 16 }
min_threads_count = ENV.fetch("RAILS_MIN_THREADS") { max_threads_count }
threads min_threads_count, max_threads_count

# Specifies the `worker_timeout` threshold that Puma will use to wait before
# terminating a worker in development environments.
#
worker_timeout 3600 if ENV.fetch("RAILS_ENV", "development") == "development"

# Specifies the `port` that Puma will listen on to receive requests; default is 3000.
#
# port ENV.fetch("PORT") { 3000 }

# Bind on all interfaces: inside a container the host reaches us via the
# published port, so a loopback bind would make the app unreachable.
bind "tcp://#{ENV.fetch('BIND', '0.0.0.0')}:#{ENV['PORT'] || 3000}"


# Specifies the `environment` that Puma will run in.
#
environment ENV.fetch("RAILS_ENV") { "development" }

# Specifies the `pidfile` that Puma will use.
pidfile ENV.fetch("PIDFILE") { "tmp/pids/server.pid" }

# Specifies the number of `workers` to boot in clustered mode.
# Workers are forked web server processes. If using threads and workers together
# the concurrency of the application would be max `threads` * `workers`.
#
# MRI executes Ruby on one core per process, so however many threads run above,
# a single process caps the whole app at one core. Two workers in production;
# development keeps single mode, where 0 means no cluster at all.
default_workers = ENV.fetch('RAILS_ENV', 'development') == 'production' ? 2 : 0
workers ENV.fetch('WEB_CONCURRENCY') { default_workers }.to_i

# No preload_app!: phased restarts (SIGUSR1) replace workers one at a time with
# the listener kept open — a restart without dropped requests — and they only
# work when each worker can boot the app itself rather than inherit it from a
# fork.

# Allow puma to be restarted by `bin/rails restart` command.
plugin :tmp_restart

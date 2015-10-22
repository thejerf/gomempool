Release v1.0.0
--------------

* Per [Issue #1](https://github.com/thejerf/gomempool/issues/1),
  requesting a buffer larger than what the pool is willing to manage
  now returns an unmanaged buffer instead.

  This also has the happy side-effect of further harmonizing the
  nil Pool and the real Pool, as the nil pool already handed out
  Allocators that wouldn't panic if the allocation was "too large".

  If you care about whether you're getting an unmanaged buffer, new
  methods MaxSize and MinSize are available on the pool now.

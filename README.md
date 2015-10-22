gomempool
=========

[![Build Status](https://travis-ci.org/thejerf/gomempool.png?branch=master)](https://travis-ci.org/thejerf/gomempool)

A []byte pool implementation for Go.

Go network programs can get into a bit of garbage collection trouble if
they are constantly allocating buffers of []byte to process network
requests. This library gives you an interface you can program to to
easily re-use []bytes without too much additional work. It also
provides statistics so you can query how well the Pool is working for you.

This is fully covered with [godoc](http://godoc.org/github.com/thejerf/gomempool),
including examples, motivation, and everything else you might otherwise
expect from a README.md on GitHub. (DRY.)

This is currently at version 1.0.0, and can imported via
gopkg.in/thejerf/gomempool.v1 if you like. [Semantic
versioning](http://semver.org/spec/v2.0.0.html) will be used for
version numbers.

Code Signing
------------

Starting with commit ff6f742, I will be signing this repository
with the ["jerf" keybase account](https://keybase.io/jerf).

sync.Pool
---------

"What about sync.Pool?" sync.Pool turns out to solve a different problem.
sync.Pool focuses on having a pool of otherwise indistinguishable objects.
gomempool specifically focuses on []bytes of potentially different sizes.
After some analysis, I don't see a reason to even use sync.Pool, because
there's not much it could improve in gomempool. gomempool and sync.Pool
turn out not to overlap at all.

(Also I've at least picked up rumors that the core devs have been
underwhelmed by sync.Pool. gomempool solves a real, if obscure,
problem. You probably don't need gomempool, but there _are_ real
problems it solves.)

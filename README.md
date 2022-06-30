gomempool
---------

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

This is currently at version 1.0.0. [Semantic
versioning](http://semver.org/spec/v2.0.0.html) will be used for
version numbers.

Code Signing
------------

Starting with commit f94a124, I will be signing this repository
with the ["jerf" keybase account](https://keybase.io/jerf). If you are viewing
this repository through GitHub, you should see the commits as showing as
"verified" in the commit view.

(Bear in mind that due to the nature of how git commit signing works, there
may be runs of unverified commits; what matters is that the top one is
signed.)

sync.Pool
---------

"What about sync.Pool?" sync.Pool efficiently pools a homogeneous collection
of objects, whereas the focus of gomempool here is explicitly on
heterogeneous []bytes. They don't overlap much.

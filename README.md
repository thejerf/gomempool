# gomempool

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

This is not currently tagged with particular git tags for Go as this is
currently considered to be alpha code. As I move this into production and
feel more confident about it, I'll give it relevant tags.

# Changelog

## [0.2.0](https://github.com/wlix13/Orrery/compare/v0.1.2...v0.2.0) (2026-08-08)


### ⚠ BREAKING CHANGES

* **collector:** online_minute/online_hour tables replaced by online_user_minute/online_user_hour, which store one row per (bucket, node, email). Nothing reads the old tables

### Features

* **collector:** count online users by identity, not per node ([ed42e92](https://github.com/wlix13/Orrery/commit/ed42e920eca7efcbdc5a9b868d182b8190caa8d4))


### Bug Fixes

* **collector:** serialize sqlite writes instead of racing for the lock ([5eb643b](https://github.com/wlix13/Orrery/commit/5eb643be9d18b538d00e2b63a47461dea720ab90))
* **dashboard:** chart online users across every hub ([207b4ac](https://github.com/wlix13/Orrery/commit/207b4ac7fd22a5a5c9976147a6175890829db8d0))

## [0.1.2](https://github.com/wlix13/Orrery/compare/v0.1.1...v0.1.2) (2026-08-01)


### Features

* **dashboard:** mark non-routable online IPs ([7338d42](https://github.com/wlix13/Orrery/commit/7338d42d3c37c83a46d3dae6e03581287910d8dd))
* **dashboard:** show on which hub each online IP was seen ([dda26be](https://github.com/wlix13/Orrery/commit/dda26beb2995bfeda15d9fcc514cbd2fcd6a7beb))


### Bug Fixes

* **collector:** scope user IP lookups to caller's fleets ([f3831ac](https://github.com/wlix13/Orrery/commit/f3831ac535b935e860640f76fafb4263f845cf0b))
* **dashboard:** update what exactly online IP list means ([9279360](https://github.com/wlix13/Orrery/commit/9279360c6a05fb144031baf19e51c5f0786b695c))

## [0.1.1](https://github.com/wlix13/Orrery/compare/v0.1.0...v0.1.1) (2026-07-24)


### Bug Fixes

* **collector:** keep store errors out of api responses ([be00cdd](https://github.com/wlix13/Orrery/commit/be00cdd635085c0d6cb742adf9f836c02ba5926a))
* **collector:** store poll failure label instead of error text ([89dfd62](https://github.com/wlix13/Orrery/commit/89dfd6220737fbaaa6a7994aa2053bb775eb0113))

## 0.1.0 (2026-07-22)


### Features

* **collector:** add config, topology and ssh dialing ([7fa1faf](https://github.com/wlix13/Orrery/commit/7fa1fafb44dda91ecea948c819749742cce7b8c3))
* **collector:** add poller, http api and cli ([4015378](https://github.com/wlix13/Orrery/commit/4015378029ae3e957066cfbcbf0eeaa036f70e56))
* **collector:** add store contract with sqlite and mongo backends ([9d3f7df](https://github.com/wlix13/Orrery/commit/9d3f7df6f366735f800eb41cef17b83d467436a0))
* **collector:** add xray stats client ([546cb3a](https://github.com/wlix13/Orrery/commit/546cb3ad4aee7e9bf198b071e36cac93805df750))
* **dashboard:** add operator dashboard SPA ([d5435d6](https://github.com/wlix13/Orrery/commit/d5435d680a74077bf373e32723027a04eeff8189))
* **worker:** allow serving dashboard from Cloudflare Workers ([8fced1d](https://github.com/wlix13/Orrery/commit/8fced1dfbd3769fd0de5438392061a0c6887f2c7))

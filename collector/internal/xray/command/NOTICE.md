# Vendored Xray-core StatsService stubs

`command.proto`, `command.pb.go`, and `command_grpc.pb.go` are copied verbatim from [Xray-core](https://github.com/XTLS/Xray-core) (`app/stats/command/`, v26.7.11) and remain licensed under the **Mozilla Public License 2.0** (see Xray-core's LICENSE).

They are vendored so Orrery can speak `xray.app.stats.command.StatsService` over gRPC without depending on the full xray-core module graph - the proto has no imports, so these three files are self-contained.

To update: copy the same three files from a newer Xray-core checkout.
Do not edit by hand.

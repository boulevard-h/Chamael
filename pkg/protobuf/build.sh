PROTO_DIR="$HOME/Chamael/pkg/protobuf"

protoc --proto_path="$PROTO_DIR" \
    --go_out="$PROTO_DIR" --go_opt=paths=source_relative \
    --go-grpc_out="$PROTO_DIR" --go-grpc_opt=paths=source_relative \
    Message.proto

# protoc-gen-go v1.36+ may emit unsafe.StringData, which requires Go >= 1.20.
# This repo targets Go 1.19/1.18, so normalize the generated file to avoid that API.
python3 - <<'PY'
from __future__ import annotations

from pathlib import Path

pb = Path.home() / "Chamael" / "pkg" / "protobuf" / "Message.pb.go"
src = pb.read_text(encoding="utf-8")

src = src.replace(
    "protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_Message_proto_rawDesc), len(file_Message_proto_rawDesc)))",
    "protoimpl.X.CompressGZIP([]byte(file_Message_proto_rawDesc))",
)
src = src.replace(
    "RawDescriptor: unsafe.Slice(unsafe.StringData(file_Message_proto_rawDesc), len(file_Message_proto_rawDesc)),",
    "RawDescriptor: []byte(file_Message_proto_rawDesc),",
)

# If we've removed all unsafe.* uses, drop the import.
if "unsafe." not in src:
    src = src.replace('\n\tunsafe "unsafe"\n', "\n")

pb.write_text(src, encoding="utf-8")
PY

gofmt -w "$PROTO_DIR/Message.pb.go"

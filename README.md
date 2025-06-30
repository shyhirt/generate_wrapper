# generate_wrapper

🛠️ A tool to generate Go wrappers for gRPC client interfaces and Valkey (Redis) access using SQLC and AST parsing.

## 🚀 Features

- Wraps `.pb.go` gRPC client interfaces
- Supports SQLC-generated queries
- Outputs clean, testable wrapper code
- Easy integration into your Go microservices

## 📦 Usage

```bash
go run generate_wrapper.go <package-path> <out-path>

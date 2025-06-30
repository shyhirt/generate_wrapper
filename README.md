# generate_wrapper

🛠️ A tool to generate Go wrappers for client interfaces and Valkey (Redis) access using SQLC and AST parsing.

![Go](https://img.shields.io/badge/Go-1.22-blue)
![License](https://img.shields.io/github/license/youruser/generate_wrapper)
![Stars](https://img.shields.io/github/stars/youruser/generate_wrapper?style=social)

---

## 🚀 Features

- ✅ Supports SQLC-generated queries
- ✅ Outputs clean, testable wrapper code
- ✅ Easy integration into Go microservices

---

## 📦 Usage

```bash
go run generate_wrapper.go <package-path> <out-path>

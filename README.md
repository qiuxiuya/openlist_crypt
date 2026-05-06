# Openlist-crypt

基于 [openlist_crypt](https://github.com/OpenListTeam/OpenList/tree/main/drivers/crypt) 的独立命令行程序。

支持使用 OpenList / rclone crypt 兼容配置，对文件名、文件夹名和文件内容进行解密或加密。

## 配置文件

复制配置示例：

```bash
cp config.example.toml config.toml
```

配置内容示例：

```toml
password = "password"
salt = "salt"
filename_encoding = "base64"
folder_name_encryption = true
file_name_encryption = "standard"
encrypted_suffix = ".bin"
```

字段说明：

| 字段 | 说明 |
| --- | --- |
| `password` | crypt 密码，支持明文、rclone obscure 值、OpenList `___Obfuscated___` 值 |
| `salt` | crypt 盐值，支持明文、rclone obscure 值、OpenList `___Obfuscated___` 值 |
| `filename_encoding` | 文件名编码，常用值为 `base64`、`base32`、`base32768` |
| `folder_name_encryption` | 是否启用文件夹名称加密 |
| `file_name_encryption` | 文件名加密方式，常用值为 `standard`、`off`、`obfuscate` |
| `encrypted_suffix` | 加密后缀，例如 `.bin` |

## 命令参数

```bash
crypt [参数]
```

| 参数 | 说明 |
| --- | --- |
| `-c` | TOML 配置文件路径，默认 `./config.toml` |
| `-d` | 输入文件或目录路径 |
| `-n` | 只处理文件名并打印结果，不处理文件内容 |
| `-o` | 输出目录；不传时使用默认输出规则 |
| `-r` | 反向模式，加上后执行加密；不加时执行解密 |

## 解密用法

### 只解密文件名

```bash
./crypt-linux-amd64 -c config.toml -n encrypted-name.bin
```

输出：

```text
original-name.ext
```

### 解密单文件

```bash
./crypt-linux-amd64 -c config.toml -d /path/to/encrypted-name.bin
```

默认输出到传入文件同目录，文件名为解密后的原文件名。

也可以指定输出目录：

```bash
./crypt-linux-amd64 -c config.toml -d /path/to/encrypted-name.bin -o /path/to/output
```

### 解密目录

```bash
./crypt-linux-amd64 -c config.toml -d /path/to/encrypted-dir
```

默认输出规则：

- 如果 `folder_name_encryption = true`，输出到同级的解密后目录名
- 如果 `folder_name_encryption = false`，输出到同级的 `d_<传入目录名>`

也可以指定输出目录：

```bash
./crypt-linux-amd64 -c config.toml -d /path/to/encrypted-dir -o /path/to/output-dir
```

## 加密用法

加上 `-r` 后进入反向模式，即加密。

### 只加密文件名

```bash
./crypt-linux-amd64 -c config.toml -n original-name.ext -r
```

输出：

```text
encrypted-name.bin
```

### 加密单文件

```bash
./crypt-linux-amd64 -c config.toml -d /path/to/original-name.ext -r
```

默认输出到传入文件同目录，文件名为加密后的文件名。

也可以指定输出目录：

```bash
./crypt-linux-amd64 -c config.toml -d /path/to/original-name.ext -o /path/to/output -r
```

### 加密目录

```bash
./crypt-linux-amd64 -c config.toml -d /path/to/original-dir -r
```

默认输出规则：

- 如果 `folder_name_encryption = true`，输出到同级的加密后目录名
- 如果 `folder_name_encryption = false`，输出到同级的 `e_<传入目录名>`

也可以指定输出目录：

```bash
./crypt-linux-amd64 -c config.toml -d /path/to/original-dir -o /path/to/output-dir -r
```

## 注意事项

- 配置必须与原 OpenList crypt 存储配置一致，否则无法正确解密
- 如果文件名或目录名加密配置不一致，路径还原会失败
- 加密与解密都会保留原始文件，不会覆盖输入文件，除非输出路径刚好指向已有文件
- 使用 `-o` 时请确认输出目录中没有同名文件，避免覆盖


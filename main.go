package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	rcCrypt "github.com/rclone/rclone/backend/crypt"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/config/obscure"
)

const obfuscatedPrefix = "___Obfuscated___"

type Config struct {
	Password             string `toml:"password"`
	Salt                 string `toml:"salt"`
	FileNameEncoding     string `toml:"filename_encoding"`
	FolderNameEncryption bool   `toml:"folder_name_encryption"`
	FileNameEncryption   string `toml:"file_name_encryption"`
	EncryptedSuffix      string `toml:"encrypted_suffix"`
}

func main() {
	configPath := flag.String("c", "config.toml", "TOML 配置文件路径")
	dataPath := flag.String("d", "", "输入文件或目录路径；默认解密，带 -r 时加密")
	namePath := flag.String("n", "", "只处理文件名并输出结果；默认解密文件名，带 -r 时加密文件名")
	outPath := flag.String("o", "", "输出路径；不传时单文件输出到同目录，目录输出到同级目录")
	reverse := flag.Bool("r", false, "反向模式：加密文件名、文件内容和目录结构")
	flag.Parse()

	if *dataPath == "" && *namePath == "" {
		fatalf("必须通过 -d 指定文件或目录，或通过 -n 指定文件名")
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fatalf("读取配置失败: %v", err)
	}

	cipher, err := newCipher(cfg)
	if err != nil {
		fatalf("创建 cipher 失败: %v", err)
	}

	if *namePath != "" {
		name, nameErr := convertFileName(cipher, filepath.Base(*namePath), *reverse)
		if nameErr != nil {
			fatalf("处理文件名失败: %v", nameErr)
		}
		fmt.Println(name)
		return
	}

	info, err := os.Stat(*dataPath)
	if err != nil {
		fatalf("读取输入路径失败: %v", err)
	}

	if info.IsDir() {
		outDir, outErr := resolveOutputDir(cipher, cfg, *dataPath, *outPath, *reverse)
		if outErr != nil {
			fatalf("解析输出目录失败: %v", outErr)
		}
		if *reverse {
			err = encodeDir(context.Background(), cipher, *dataPath, outDir)
		} else {
			err = decodeDir(context.Background(), cipher, *dataPath, outDir)
		}
	} else {
		name, nameErr := convertFileName(cipher, filepath.Base(*dataPath), *reverse)
		if nameErr != nil {
			fatalf("处理文件名失败: %v", nameErr)
		}
		outFile := filepath.Join(filepath.Dir(*dataPath), name)
		if *outPath != "" {
			outFile = filepath.Join(*outPath, name)
		}
		if *reverse {
			err = encodeFile(context.Background(), cipher, *dataPath, outFile, info.Mode(), info.ModTime().UnixNano())
		} else {
			err = decodeFile(context.Background(), cipher, *dataPath, outFile, info.Mode(), info.ModTime().UnixNano())
		}
	}
	if err != nil {
		if *reverse {
			fatalf("加密失败: %v", err)
		}
		fatalf("解密失败: %v", err)
	}
}

func loadConfig(path string) (Config, error) {
	cfg := Config{
		FileNameEncoding:     "base64",
		FolderNameEncryption: true,
		FileNameEncryption:   "standard",
		EncryptedSuffix:      ".bin",
	}
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.Password == "" {
		return Config{}, errors.New("password 不能为空")
	}
	if cfg.FileNameEncoding == "" {
		cfg.FileNameEncoding = "base64"
	}
	if cfg.FileNameEncryption == "" {
		cfg.FileNameEncryption = "standard"
	}
	if cfg.EncryptedSuffix == "" {
		cfg.EncryptedSuffix = ".bin"
	}
	return cfg, nil
}

func newCipher(cfg Config) (*rcCrypt.Cipher, error) {
	password, err := normalizeSecret(cfg.Password)
	if err != nil {
		return nil, fmt.Errorf("password: %w", err)
	}
	salt, err := normalizeSecret(cfg.Salt)
	if err != nil {
		return nil, fmt.Errorf("salt: %w", err)
	}

	m := configmap.Simple{
		"password":                  password,
		"password2":                 salt,
		"filename_encryption":       cfg.FileNameEncryption,
		"directory_name_encryption": fmt.Sprintf("%t", cfg.FolderNameEncryption),
		"filename_encoding":         cfg.FileNameEncoding,
		"suffix":                    cfg.EncryptedSuffix,
		"pass_bad_blocks":           "",
	}
	return rcCrypt.NewCipher(m)
}

func normalizeSecret(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, obfuscatedPrefix) {
		return strings.TrimPrefix(value, obfuscatedPrefix), nil
	}
	if _, err := obscure.Reveal(value); err == nil {
		return value, nil
	}
	return obscure.Obscure(value)
}

func resolveOutputDir(cipher *rcCrypt.Cipher, cfg Config, dataPath, outPath string, encrypt bool) (string, error) {
	if outPath != "" {
		return outPath, nil
	}

	parent := filepath.Dir(dataPath)
	base := filepath.Base(dataPath)
	if cfg.FolderNameEncryption {
		if encrypt {
			return filepath.Join(parent, cipher.EncryptDirName(base)), nil
		}
		name, err := cipher.DecryptDirName(base)
		if err != nil {
			return "", err
		}
		return filepath.Join(parent, name), nil
	}
	if encrypt {
		return filepath.Join(parent, "e_"+base), nil
	}
	return filepath.Join(parent, "d_"+base), nil
}

func decodeDir(ctx context.Context, cipher *rcCrypt.Cipher, inRoot, outRoot string) error {
	return filepath.WalkDir(inRoot, func(src string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if src == inRoot {
			return os.MkdirAll(outRoot, 0o755)
		}

		rel, err := filepath.Rel(inRoot, src)
		if err != nil {
			return err
		}
		dstRel, err := convertRelPath(cipher, rel, entry.IsDir(), false)
		if err != nil {
			return fmt.Errorf("解密路径 %q 失败: %w", rel, err)
		}
		dst := filepath.Join(outRoot, dstRel)

		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(dst, info.Mode())
		}
		return decodeFile(ctx, cipher, src, dst, info.Mode(), info.ModTime().UnixNano())
	})
}

func encodeDir(ctx context.Context, cipher *rcCrypt.Cipher, inRoot, outRoot string) error {
	return filepath.WalkDir(inRoot, func(src string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if src == inRoot {
			return os.MkdirAll(outRoot, 0o755)
		}

		rel, err := filepath.Rel(inRoot, src)
		if err != nil {
			return err
		}
		dstRel, err := convertRelPath(cipher, rel, entry.IsDir(), true)
		if err != nil {
			return fmt.Errorf("加密路径 %q 失败: %w", rel, err)
		}
		dst := filepath.Join(outRoot, dstRel)

		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(dst, info.Mode())
		}
		return encodeFile(ctx, cipher, src, dst, info.Mode(), info.ModTime().UnixNano())
	})
}

func convertRelPath(cipher *rcCrypt.Cipher, rel string, leafIsDir bool, encrypt bool) (string, error) {
	parts := splitPath(rel)
	for i, part := range parts {
		isLeaf := i == len(parts)-1
		if isLeaf && !leafIsDir {
			name, err := convertFileName(cipher, part, encrypt)
			if err != nil {
				return "", err
			}
			parts[i] = name
			continue
		}
		name, err := convertDirName(cipher, part, encrypt)
		if err != nil {
			return "", err
		}
		parts[i] = name
	}
	return filepath.Join(parts...), nil
}

func splitPath(path string) []string {
	clean := filepath.Clean(path)
	if clean == "." || clean == string(filepath.Separator) {
		return nil
	}
	return strings.Split(clean, string(filepath.Separator))
}

func convertFileName(cipher *rcCrypt.Cipher, name string, encrypt bool) (string, error) {
	if encrypt {
		return cipher.EncryptFileName(name), nil
	}
	return cipher.DecryptFileName(name)
}

func convertDirName(cipher *rcCrypt.Cipher, name string, encrypt bool) (string, error) {
	if encrypt {
		return cipher.EncryptDirName(name), nil
	}
	return cipher.DecryptDirName(name)
}

func decodeFile(ctx context.Context, cipher *rcCrypt.Cipher, src, dst string, mode os.FileMode, modTimeUnixNano int64) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	reader, err := cipher.DecryptData(in)
	if err != nil {
		return err
	}
	defer reader.Close()

	return writeStream(dst, mode, modTimeUnixNano, reader)
}

func encodeFile(ctx context.Context, cipher *rcCrypt.Cipher, src, dst string, mode os.FileMode, modTimeUnixNano int64) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	reader, err := cipher.EncryptData(in)
	if err != nil {
		return err
	}

	return writeStream(dst, mode, modTimeUnixNano, reader)
}

func writeStream(dst string, mode os.FileMode, modTimeUnixNano int64, reader io.Reader) error {
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, reader)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Chtimes(dst, unixNanoToTime(modTimeUnixNano), unixNanoToTime(modTimeUnixNano))
}

func unixNanoToTime(n int64) (t time.Time) {
	return time.Unix(0, n)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

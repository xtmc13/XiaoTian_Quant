package agentwecom

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
)

// WXBizMsgCrypt 企业微信回调消息加解密（纯标准库实现）：
//   - 签名：sha1(sort(token, timestamp, nonce, encrypt)) 十六进制
//   - 密钥：base64decode(aesKey + "=")（43 字符的 aesKey 补 "=" 得 32 字节密钥）
//   - 加密：AES-256-CBC，IV = key[:16]，PKCS7 填充（块大小 32）
//   - 明文结构：random(16) | msgLen(4B 大端) | msg | corpID（receiveid）

// msgSignature 计算回调签名（四个串字典序拼接后 sha1 十六进制）。
func msgSignature(token, timestamp, nonce, encrypt string) string {
	parts := []string{token, timestamp, nonce, encrypt}
	sort.Strings(parts)
	h := sha1.New()
	for _, p := range parts {
		h.Write([]byte(p))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// decodeAESKey EncodingAESKey（43 字符）→ 32 字节 AES 密钥。
func decodeAESKey(aesKey string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(aesKey + "=")
	if err != nil {
		return nil, fmt.Errorf("EncodingAESKey 解码失败: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("EncodingAESKey 长度错误（解码后 %d 字节，应为 32）", len(key))
	}
	return key, nil
}

// decryptMsg 解密密文消息，返回明文与携带的 corpID（调用方校验）。
func decryptMsg(key []byte, cipherB64 string) (msg []byte, corpID string, err error) {
	raw, err := base64.StdEncoding.DecodeString(cipherB64)
	if err != nil {
		return nil, "", fmt.Errorf("密文 base64 解码失败: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, "", err
	}
	if len(raw) == 0 || len(raw)%block.BlockSize() != 0 {
		return nil, "", errors.New("密文长度非块对齐")
	}
	plain := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, key[:16]).CryptBlocks(plain, raw)
	plain = pkcs7Unpad(plain, 32)
	if plain == nil || len(plain) < 20 {
		return nil, "", errors.New("PKCS7 去填充失败")
	}
	msgLen := binary.BigEndian.Uint32(plain[16:20])
	if int(msgLen) > len(plain)-20 {
		return nil, "", errors.New("明文长度字段越界")
	}
	msg = plain[20 : 20+msgLen]
	corpID = string(plain[20+msgLen:])
	return msg, corpID, nil
}

// encryptMsg 加密明文消息（GET 验证响应构造与测试夹具共用）。
func encryptMsg(key []byte, msg []byte, corpID string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	rnd := make([]byte, 16)
	if _, err := rand.Read(rnd); err != nil {
		return "", err
	}
	buf := make([]byte, 0, 20+len(msg)+len(corpID)+32)
	buf = append(buf, rnd...)
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(msg)))
	buf = append(buf, lenBuf[:]...)
	buf = append(buf, msg...)
	buf = append(buf, corpID...)
	buf = pkcs7Pad(buf, 32)
	out := make([]byte, len(buf))
	cipher.NewCBCEncrypter(block, key[:16]).CryptBlocks(out, buf)
	return base64.StdEncoding.EncodeToString(out), nil
}

// pkcs7Pad PKCS7 填充（blockSize 按企业微信约定取 32）。
func pkcs7Pad(data []byte, blockSize int) []byte {
	pad := blockSize - len(data)%blockSize
	out := make([]byte, len(data)+pad)
	copy(out, data)
	for i := len(data); i < len(out); i++ {
		out[i] = byte(pad)
	}
	return out
}

// pkcs7Unpad 去填充；填充非法返回 nil。
func pkcs7Unpad(data []byte, blockSize int) []byte {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil
	}
	pad := int(data[len(data)-1])
	if pad < 1 || pad > blockSize || pad > len(data) {
		return nil
	}
	for _, b := range data[len(data)-pad:] {
		if int(b) != pad {
			return nil
		}
	}
	return data[:len(data)-pad]
}

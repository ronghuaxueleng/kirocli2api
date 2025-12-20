package Utils

import "strings"

func getExtension(mediaType string) string {
	if strings.Contains(mediaType, "/") {
		parts := strings.Split(mediaType, "/")
		return parts[len(parts)-1]
	}
	return "png"
}

func getExtensionFromBase64Data(data string) string {
	prefix := "data:"
	if strings.HasPrefix(data, prefix) {
		semicolonIndex := strings.Index(data, ";")
		if semicolonIndex != -1 {
			mediaType := data[len(prefix):semicolonIndex]
			return getExtension(mediaType)
		}
	}
	return "png"
}

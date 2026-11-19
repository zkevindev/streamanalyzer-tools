package diagnostic

import (
	"encoding/base64"
	"fmt"
)

// This label accompanies the following image block. It binds visible content
// to an existing case image ID, rather than inventing new finding IDs.
func modelImageLabel(image ModelImage) string {
	label := fmt.Sprintf("图片 %s；案例 %s；产物 %s", image.Reference.ID, image.Reference.CaseID, image.Reference.Name)
	if image.Reference.MediaTimeMS != nil {
		label += fmt.Sprintf("；媒体时间 %d ms", *image.Reference.MediaTimeMS)
	}
	return label + "。这是派生源媒体画面，不代表浏览器当时已呈现，也不能单独证明卡顿根因。"
}

func modelImageBase64(image ModelImage) string {
	return base64.StdEncoding.EncodeToString(image.JPEG)
}

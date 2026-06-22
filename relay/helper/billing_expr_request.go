package helper

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

func ResolveIncomingBillingExprRequestInput(c *gin.Context, info *relaycommon.RelayInfo) (billingexpr.RequestInput, error) {
	if info != nil && info.BillingRequestInput != nil {
		input := cloneRequestInput(*info.BillingRequestInput)
		merged := cloneStringMap(info.RequestHeaders)
		for k, v := range input.Headers {
			merged[k] = v
		}
		input.Headers = merged
		return input, nil
	}

	input := billingexpr.RequestInput{}
	if info != nil {
		input.Headers = cloneStringMap(info.RequestHeaders)
	}

	bodyBytes, err := readIncomingBillingExprBody(c)
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	input.Body = withImageBillingParams(bodyBytes, info)
	return input, nil
}

func BuildBillingExprRequestInputFromRequest(request dto.Request, headers map[string]string) (billingexpr.RequestInput, error) {
	input := billingexpr.RequestInput{
		Headers: cloneStringMap(headers),
	}
	if request == nil {
		return input, nil
	}

	bodyBytes, err := common.Marshal(request)
	if err != nil {
		return billingexpr.RequestInput{}, err
	}
	input.Body = withImageBillingParamsFromRequest(bodyBytes, request)
	return input, nil
}

func withImageBillingParams(body []byte, info *relaycommon.RelayInfo) []byte {
	if info == nil {
		return body
	}
	return withImageBillingParamsFromRequest(body, info.Request)
}

func withImageBillingParamsFromRequest(body []byte, request dto.Request) []byte {
	imageReq, ok := request.(*dto.ImageRequest)
	if !ok {
		return body
	}

	bodyMap := map[string]interface{}{}
	if len(body) > 0 {
		if err := common.Unmarshal(body, &bodyMap); err != nil {
			return body
		}
	}
	bodyMap["billing_size"] = imageBillingSize(imageReq.Size)

	updatedBody, err := common.Marshal(bodyMap)
	if err != nil {
		return body
	}
	return updatedBody
}

func imageBillingSize(size string) string {
	size = strings.TrimSpace(size)
	switch strings.ToLower(size) {
	case "1k":
		return "1K"
	case "2k":
		return "2K"
	case "4k":
		return "4K"
	}

	parts := strings.Split(strings.ToLower(size), "x")
	if len(parts) != 2 {
		return "2K"
	}
	width, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || width <= 0 || height <= 0 {
		return "2K"
	}

	maxSide := width
	if height > maxSide {
		maxSide = height
	}
	if maxSide <= 1024 {
		return "1K"
	}
	if maxSide <= 2048 {
		return "2K"
	}
	return "4K"
}

func readIncomingBillingExprBody(c *gin.Context) ([]byte, error) {
	if c == nil || c.Request == nil || !isJSONContentType(c.Request.Header.Get("Content-Type")) {
		return nil, nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	return storage.Bytes()
}

func cloneRequestInput(src billingexpr.RequestInput) billingexpr.RequestInput {
	input := billingexpr.RequestInput{
		Headers: cloneStringMap(src.Headers),
	}
	if len(src.Body) > 0 {
		input.Body = append([]byte(nil), src.Body...)
	}
	return input
}

func isJSONContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return strings.HasPrefix(contentType, "application/json")
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return map[string]string{}
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		if strings.TrimSpace(key) == "" {
			continue
		}
		dst[key] = value
	}
	return dst
}

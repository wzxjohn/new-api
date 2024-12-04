package controller

import (
	"fmt"
	"github.com/stripe/stripe-go/v81/checkout/session"
	"io"
	"log"
	"net/http"
	"one-api/common"
	"one-api/constant"
	"one-api/model"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/webhook"
)

func RequestStripe(c *gin.Context) {
	var req PayRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(200, gin.H{"message": err.Error(), "data": 10})
		return
	}
	if !constant.PaymentEnabled {
		c.JSON(200, gin.H{"message": "error", "data": "管理员未开启在线支付"})
		return
	}
	if req.PaymentMethod != "stripe" {
		c.JSON(200, gin.H{"message": "error", "data": "不支持的支付渠道"})
		return
	}
	if req.Amount < constant.MinTopUp {
		c.JSON(200, gin.H{"message": fmt.Sprintf("充值数量不能小于 %d", constant.MinTopUp), "data": 10})
		return
	}
	if req.Amount > 10000 {
		c.JSON(200, gin.H{"message": "充值数量不能大于 10000", "data": 10})
		return
	}

	id := c.GetInt("id")
	user, _ := model.GetUserById(id, false)
	chargedMoney := GetChargedAmount(float64(req.Amount), *user)

	reference := fmt.Sprintf("new-api-ref-%d-%d-%s", user.Id, time.Now().UnixMilli(), common.RandomString(4))
	referenceId := "ref_" + common.Sha1(reference)

	payLink, err := genStripeLink(referenceId, user.StripeCustomer, user.Email, int64(req.Amount))
	if err != nil {
		log.Println("获取Stripe Checkout支付链接失败", err)
		c.JSON(200, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	topUp := &model.TopUp{
		UserId:     id,
		Amount:     req.Amount,
		Money:      chargedMoney,
		TradeNo:    referenceId,
		CreateTime: time.Now().Unix(),
		Status:     common.TopUpStatusPending,
	}
	err = topUp.Insert()
	if err != nil {
		c.JSON(200, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}
	c.JSON(200, gin.H{
		"message": "success",
		"data": gin.H{
			"payLink": payLink,
		},
	})
}

func StripeWebhook(c *gin.Context) {
	payload, err := io.ReadAll(c.Request.Body)
	if err != nil {
		log.Printf("解析Stripe Webhook参数失败: %v\n", err)
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}

	signature := c.GetHeader("Stripe-Signature")
	endpointSecret := constant.StripeWebhookSecret
	event, err := webhook.ConstructEventWithOptions(payload, signature, endpointSecret, webhook.ConstructEventOptions{
		IgnoreAPIVersionMismatch: true,
	})

	if err != nil {
		log.Printf("Stripe Webhook验签失败: %v\n", err)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	switch event.Type {
	case stripe.EventTypeCheckoutSessionCompleted:
		sessionCompleted(event)
	case stripe.EventTypeCheckoutSessionExpired:
		sessionExpired(event)
	default:
		log.Printf("不支持的Stripe Webhook事件类型: %s\n", event.Type)
	}

	c.Status(http.StatusOK)
}

func sessionCompleted(event stripe.Event) {
	customerId := event.GetObjectValue("customer")
	referenceId := event.GetObjectValue("client_reference_id")
	status := event.GetObjectValue("status")
	if "complete" != status {
		log.Println("错误的Stripe Checkout完成状态:", status, ",", referenceId)
		return
	}

	err := model.Recharge(referenceId, customerId)
	if err != nil {
		log.Println(err.Error(), referenceId)
		return
	}

	total, _ := strconv.ParseFloat(event.GetObjectValue("amount_total"), 64)
	currency := strings.ToUpper(event.GetObjectValue("currency"))
	log.Printf("收到款项：%s, %.2f(%s)", referenceId, total/100, currency)
}

func sessionExpired(event stripe.Event) {
	referenceId := event.GetObjectValue("client_reference_id")
	status := event.GetObjectValue("status")
	if "expired" != status {
		log.Println("错误的Stripe Checkout过期状态:", status, ",", referenceId)
		return
	}

	if "" == referenceId {
		log.Println("未提供支付单号")
		return
	}

	topUp := model.GetTopUpByTradeNo(referenceId)
	if topUp == nil {
		log.Println("充值订单不存在", referenceId)
		return
	}

	if topUp.Status != common.TopUpStatusPending {
		log.Println("充值订单状态错误", referenceId)
	}

	topUp.Status = common.TopUpStatusExpired
	err := topUp.Update()
	if err != nil {
		log.Println("过期充值订单失败", referenceId, ", err:", err.Error())
		return
	}

	log.Println("充值订单已过期", referenceId)
}

func genStripeLink(referenceId string, customerId string, email string, amount int64) (string, error) {
	if !strings.HasPrefix(constant.StripeApiSecret, "sk_") {
		return "", fmt.Errorf("无效的Stripe API密钥")
	}

	stripe.Key = constant.StripeApiSecret

	params := &stripe.CheckoutSessionParams{
		ClientReferenceID: stripe.String(referenceId),
		SuccessURL:        stripe.String(constant.ServerAddress + "/log"),
		CancelURL:         stripe.String(constant.ServerAddress + "/topup"),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(constant.StripePriceId),
				Quantity: stripe.Int64(amount),
			},
		},
		Mode: stripe.String(string(stripe.CheckoutSessionModePayment)),
	}

	if "" == customerId {
		if "" != email {
			params.CustomerEmail = stripe.String(email)
		}

		params.CustomerCreation = stripe.String(string(stripe.CheckoutSessionCustomerCreationAlways))
	} else {
		params.Customer = stripe.String(customerId)
	}

	result, err := session.New(params)
	if err != nil {
		return "", err
	}

	return result.URL, nil
}

func GetChargedAmount(count float64, user model.User) float64 {
	topUpGroupRatio := common.GetTopupGroupRatio(user.Group)
	if topUpGroupRatio == 0 {
		topUpGroupRatio = 1
	}

	return count * topUpGroupRatio
}

package controller

import (
	"fmt"
	"one-api/common"
	"one-api/constant"
	"one-api/model"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"
)

type PayAdaptor interface {
	RequestPay(c *gin.Context, req *PayRequest)
}

var (
	payNameAdaptorMap = map[string]PayAdaptor{}
)

type PayRequest struct {
	Amount        int    `json:"amount"`
	PaymentMethod string `json:"payment_method"`
	TopUpCode     string `json:"top_up_code"`
}

type AmountRequest struct {
	Amount    int    `json:"amount"`
	TopUpCode string `json:"top_up_code"`
}

func RequestPay(c *gin.Context) {
	var req PayRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(200, gin.H{"message": "error", "data": "参数错误"})
		return
	}

	if !constant.PaymentEnabled {
		c.JSON(200, gin.H{"message": "error", "data": "管理员未开启在线支付"})
		return
	}

	payAdaptor, ok := payNameAdaptorMap[req.PaymentMethod]
	if !ok {
		c.JSON(200, gin.H{"message": "error", "data": "不支持的支付方式"})
	}
	payAdaptor.RequestPay(c, &req)
}

func getPayMoney(amount float64, group string) float64 {
	if !common.DisplayInCurrencyEnabled {
		amount = amount / common.QuotaPerUnit
	}
	// 别问为什么用float64，问就是这么点钱没必要
	topupGroupRatio := common.GetTopupGroupRatio(group)
	if topupGroupRatio == 0 {
		topupGroupRatio = 1
	}
	payMoney := amount * constant.EpayPrice * topupGroupRatio
	return payMoney
}

func getMinTopup() int {
	minTopup := constant.MinTopUp
	if !common.DisplayInCurrencyEnabled {
		minTopup = minTopup * int(common.QuotaPerUnit)
	}
	return minTopup
}

// tradeNo lock
var orderLocks sync.Map
var createLock sync.Mutex

// LockOrder 尝试对给定订单号加锁
func LockOrder(tradeNo string) {
	lock, ok := orderLocks.Load(tradeNo)
	if !ok {
		createLock.Lock()
		defer createLock.Unlock()
		lock, ok = orderLocks.Load(tradeNo)
		if !ok {
			lock = new(sync.Mutex)
			orderLocks.Store(tradeNo, lock)
		}
	}
	lock.(*sync.Mutex).Lock()
}

// UnlockOrder 释放给定订单号的锁
func UnlockOrder(tradeNo string) {
	lock, ok := orderLocks.Load(tradeNo)
	if ok {
		lock.(*sync.Mutex).Unlock()
	}
}

func RequestAmount(c *gin.Context) {
	var req AmountRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(200, gin.H{"message": "error", "data": "参数错误"})
		return
	}

	if !constant.PaymentEnabled {
		c.JSON(200, gin.H{"message": "error", "data": "管理员未开启在线支付"})
		return
	}

	if req.Amount < getMinTopup() {
		c.JSON(200, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", getMinTopup())})
		return
	}
	id := c.GetInt("id")
	group, err := model.CacheGetUserGroup(id)
	if err != nil {
		c.JSON(200, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}
	payMoney := getPayMoney(float64(req.Amount), group)
	if payMoney <= 0.01 {
		c.JSON(200, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}
	c.JSON(200, gin.H{"message": "success", "data": strconv.FormatFloat(payMoney, 'f', 2, 64)})
}

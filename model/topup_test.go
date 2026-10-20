package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setInviteRechargeRebatePercent enables the invitation recharge rebate for a
// single test and restores the previous percentage and compliance state.
func setInviteRechargeRebatePercent(t *testing.T, percent float64) {
	t.Helper()
	originalPercent := common.InviteRechargeRebatePercent
	common.InviteRechargeRebatePercent = percent
	t.Cleanup(func() { common.InviteRechargeRebatePercent = originalPercent })

	paymentSetting := operation_setting.GetPaymentSetting()
	originalConfirmed := paymentSetting.ComplianceConfirmed
	originalVersion := paymentSetting.ComplianceTermsVersion
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalConfirmed
		paymentSetting.ComplianceTermsVersion = originalVersion
	})
}

func createRebateUser(t *testing.T, username string, inviterId int) *User {
	t.Helper()
	user := &User{
		Username:  username,
		Password:  "password",
		Status:    common.UserStatusEnabled,
		InviterId: inviterId,
		AffCode:   username + "-code",
	}
	require.NoError(t, DB.Create(user).Error)
	return user
}

func loadRebateUser(t *testing.T, userId int) User {
	t.Helper()
	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	return user
}

// The inviter of an invited user receives the configured share of the topped
// up quota, and only the first level is rewarded.
func TestCreditInviteRechargeRebateCreditsFirstLevelInviter(t *testing.T) {
	truncateTables(t)
	setInviteRechargeRebatePercent(t, 10)

	root := createRebateUser(t, "rebate-root", 0)
	inviter := createRebateUser(t, "rebate-inviter", root.Id)
	invitee := createRebateUser(t, "rebate-invitee", inviter.Id)

	creditInviteRechargeRebate(invitee.Id, 5000)

	gotInviter := loadRebateUser(t, inviter.Id)
	assert.Equal(t, 500, gotInviter.AffQuota)
	assert.Equal(t, 500, gotInviter.AffHistoryQuota)
	assert.Equal(t, 0, loadRebateUser(t, root.Id).AffQuota, "only the first level inviter is rewarded")
}

// A decimal percentage is rounded to whole quota units.
func TestCreditInviteRechargeRebateRoundsToWholeQuota(t *testing.T) {
	truncateTables(t)
	setInviteRechargeRebatePercent(t, 12.5)

	inviter := createRebateUser(t, "rebate-inviter", 0)
	invitee := createRebateUser(t, "rebate-invitee", inviter.Id)

	creditInviteRechargeRebate(invitee.Id, 1001)

	assert.Equal(t, 125, loadRebateUser(t, inviter.Id).AffQuota, "12.5% of 1001 is 125.125 and rounds to 125")
}

// A rebate derived from a large top-up must not be capped at the single
// request int32 bound, otherwise the inviter is paid less than the
// configured share.
func TestCreditInviteRechargeRebateIsNotCappedAtInt32(t *testing.T) {
	truncateTables(t)
	setInviteRechargeRebatePercent(t, 100)

	inviter := createRebateUser(t, "rebate-inviter", 0)
	invitee := createRebateUser(t, "rebate-invitee", inviter.Id)

	// 10_000_000 exceeds the int32 quota bound.
	const creditedQuota = 10_000_000
	creditInviteRechargeRebate(invitee.Id, creditedQuota)

	assert.Equal(t, creditedQuota, loadRebateUser(t, inviter.Id).AffQuota, "a 100% rebate equals the credited quota")
}

// The rebate is withheld when it is disabled, when the operator has not
// confirmed the compliance terms, and when the invitee has no inviter.
func TestCreditInviteRechargeRebateWithholds(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		truncateTables(t)
		setInviteRechargeRebatePercent(t, 0)
		inviter := createRebateUser(t, "rebate-inviter", 0)
		invitee := createRebateUser(t, "rebate-invitee", inviter.Id)

		creditInviteRechargeRebate(invitee.Id, 5000)

		assert.Equal(t, 0, loadRebateUser(t, inviter.Id).AffQuota)
	})

	t.Run("compliance not confirmed", func(t *testing.T) {
		truncateTables(t)
		setInviteRechargeRebatePercent(t, 10)
		operation_setting.GetPaymentSetting().ComplianceConfirmed = false
		inviter := createRebateUser(t, "rebate-inviter", 0)
		invitee := createRebateUser(t, "rebate-invitee", inviter.Id)

		creditInviteRechargeRebate(invitee.Id, 5000)

		assert.Equal(t, 0, loadRebateUser(t, inviter.Id).AffQuota)
	})

	t.Run("no inviter", func(t *testing.T) {
		truncateTables(t)
		setInviteRechargeRebatePercent(t, 10)
		invitee := createRebateUser(t, "rebate-invitee", 0)

		creditInviteRechargeRebate(invitee.Id, 5000)

		assert.Equal(t, 0, loadRebateUser(t, invitee.Id).AffQuota)
	})
}

// A user cannot be their own inviter, and a deleted inviter must not be
// revived or paid by the rebate.
func TestCreditInviteRechargeRebateSkipsInvalidInviter(t *testing.T) {
	t.Run("self inviter", func(t *testing.T) {
		truncateTables(t)
		setInviteRechargeRebatePercent(t, 10)
		invitee := createRebateUser(t, "rebate-self", 0)
		require.NoError(t, DB.Model(&User{}).Where("id = ?", invitee.Id).Update("inviter_id", invitee.Id).Error)

		creditInviteRechargeRebate(invitee.Id, 5000)

		assert.Equal(t, 0, loadRebateUser(t, invitee.Id).AffQuota)
	})

	t.Run("deleted inviter", func(t *testing.T) {
		truncateTables(t)
		setInviteRechargeRebatePercent(t, 10)
		inviter := createRebateUser(t, "rebate-inviter", 0)
		invitee := createRebateUser(t, "rebate-invitee", inviter.Id)
		require.NoError(t, DB.Delete(inviter).Error)

		creditInviteRechargeRebate(invitee.Id, 5000)

		var gotInviter User
		require.NoError(t, DB.Unscoped().First(&gotInviter, "id = ?", inviter.Id).Error)
		assert.Equal(t, 0, gotInviter.AffQuota)
		assert.True(t, gotInviter.DeletedAt.Valid, "the deleted inviter stays deleted")
	})
}

// The invitation quota columns are 64-bit wide. A full counter must skip the
// rebate instead of overflowing.
func TestCreditInviteRechargeRebateSkipsWhenInviterQuotaIsFull(t *testing.T) {
	truncateTables(t)
	setInviteRechargeRebatePercent(t, 10)

	inviter := createRebateUser(t, "rebate-inviter", 0)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", inviter.Id).Updates(map[string]any{
		"aff_quota":   common.MaxWalletQuota,
		"aff_history": common.MaxWalletQuota,
	}).Error)
	invitee := createRebateUser(t, "rebate-invitee", inviter.Id)

	creditInviteRechargeRebate(invitee.Id, 5000)

	assert.Equal(t, common.MaxWalletQuota, loadRebateUser(t, inviter.Id).AffQuota, "the counter must not overflow")
}

func TestRechargeEpayCreditsInviterRebate(t *testing.T) {
	truncateTables(t)
	setInviteRechargeRebatePercent(t, 10)

	inviter := createRebateUser(t, "rebate-inviter", 0)
	invitee := createRebateUser(t, "rebate-invitee", inviter.Id)

	topUp := &TopUp{
		UserId:          invitee.Id,
		Amount:          10,
		Money:           10,
		TradeNo:         "rebate-trade-no",
		PaymentMethod:   PaymentProviderEpay,
		PaymentProvider: PaymentProviderEpay,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, topUp.Insert())

	_, err := RechargeEpay(topUp.TradeNo, PaymentProviderEpay, "127.0.0.1")
	require.NoError(t, err)

	creditedQuota := int(float64(10) * common.QuotaPerUnit)
	assert.Equal(t, creditedQuota/10, loadRebateUser(t, inviter.Id).AffQuota)

	// The inviter is notified about the invitation quota they received.
	var logs []Log
	require.NoError(t, DB.Where("user_id = ? AND type = ?", inviter.Id, LogTypeSystem).Find(&logs).Error)
	require.Len(t, logs, 1, "the inviter gets exactly one rebate notification")
	assert.Contains(t, logs[0].Content, "Invitation recharge rebate")
}

func TestRedeemCreditsInviterRebate(t *testing.T) {
	userId, key := setupRedeemFixture(t, 500)
	inviter := createRebateUser(t, "rebate-inviter", 0)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", userId).Update("inviter_id", inviter.Id).Error)
	setInviteRechargeRebatePercent(t, 10)

	_, err := Redeem(key, userId)
	require.NoError(t, err)

	assert.Equal(t, 50, loadRebateUser(t, inviter.Id).AffQuota)
	assert.Equal(t, 50, loadRebateUser(t, inviter.Id).AffHistoryQuota)
}

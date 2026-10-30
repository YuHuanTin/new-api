package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSetChannelCyberGuardTogglesOnlyThatChannel(t *testing.T) {
	previousDB := DB
	previousEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() {
		DB = previousDB
		common.MemoryCacheEnabled = previousEnabled
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&Channel{}))
	DB = db
	common.MemoryCacheEnabled = false

	first := &Channel{Name: "first", Setting: nil}
	second := &Channel{Name: "second"}
	require.NoError(t, db.Create(first).Error)
	require.NoError(t, db.Create(second).Error)

	require.NoError(t, SetChannelCyberGuard(first.Id, true))

	var stored Channel
	require.NoError(t, db.First(&stored, first.Id).Error)
	assert.True(t, stored.GetSetting().CyberGuardEnabled)
	var untouched Channel
	require.NoError(t, db.First(&untouched, second.Id).Error)
	assert.False(t, untouched.GetSetting().CyberGuardEnabled, "其它渠道不受影响")

	channels, err := ListCyberGuardChannels()
	require.NoError(t, err)
	require.Len(t, channels, 1)
	assert.Equal(t, first.Id, channels[0].Id)

	require.NoError(t, SetChannelCyberGuard(first.Id, false))
	channels, err = ListCyberGuardChannels()
	require.NoError(t, err)
	assert.Empty(t, channels)
}

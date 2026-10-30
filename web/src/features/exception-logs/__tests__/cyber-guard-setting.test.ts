import { describe, expect, it } from 'vitest'

import { readCyberGuardSetting, writeCyberGuardSetting } from '../api'

describe('cyber guard setting', () => {
  it('reads the shared thresholds from the option strings', () => {
    const setting = readCyberGuardSetting({
      'cyber_setting.auto_ban': 'false',
      'cyber_setting.window_minutes': '15',
      'cyber_setting.threshold': '4',
    })

    expect(setting).toEqual({
      auto_ban: false,
      window_minutes: 15,
      threshold: 4,
    })
  })

  it('falls back to safe defaults for missing options', () => {
    const setting = readCyberGuardSetting({})

    expect(setting.auto_ban).toBe(true)
    expect(setting.window_minutes).toBe(10)
    expect(setting.threshold).toBe(3)
  })

  // 开关在渠道上，共用的参数里没有「总开关」
  it('writes only the shared thresholds back as option strings', () => {
    expect(
      writeCyberGuardSetting({
        auto_ban: false,
        window_minutes: 20,
        threshold: 5,
      })
    ).toEqual({
      'cyber_setting.auto_ban': 'false',
      'cyber_setting.window_minutes': '20',
      'cyber_setting.threshold': '5',
    })
  })
})

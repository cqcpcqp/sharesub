import type { GlobalThemeOverrides } from 'naive-ui'

const inputControlOverrides = {
  heightMedium: '40px',
  borderRadius: '7px',
  color: 'var(--surface-soft)',
  colorFocus: 'var(--surface-soft)',
  colorDisabled: 'var(--surface-hover)',
  textColor: 'var(--ink)',
  textColorDisabled: 'var(--muted-light)',
  placeholderColor: 'var(--muted-light)',
  placeholderColorDisabled: 'var(--muted-light)',
  border: '1px solid var(--line-strong)',
  borderHover: '1px solid var(--primary)',
  borderFocus: '1px solid var(--primary)',
  borderDisabled: '1px solid var(--line)',
  boxShadowFocus: '0 0 0 3px rgba(223, 89, 67, .12)',
}

const selectionControlOverrides = {
  heightMedium: '40px',
  borderRadius: '7px',
  color: 'var(--surface-soft)',
  colorActive: 'var(--surface-soft)',
  colorDisabled: 'var(--surface-hover)',
  textColor: 'var(--ink)',
  textColorDisabled: 'var(--muted-light)',
  placeholderColor: 'var(--muted-light)',
  placeholderColorDisabled: 'var(--muted-light)',
  border: '1px solid var(--line-strong)',
  borderHover: '1px solid var(--primary)',
  borderActive: '1px solid var(--primary)',
  borderFocus: '1px solid var(--primary)',
  boxShadowHover: 'none',
  boxShadowActive: '0 0 0 3px rgba(223, 89, 67, .12)',
  boxShadowFocus: '0 0 0 3px rgba(223, 89, 67, .12)',
}

const baseThemeOverrides: GlobalThemeOverrides = {
  common: {
    primaryColor: '#df5943',
    primaryColorHover: '#c94734',
    primaryColorPressed: '#b63c2c',
    primaryColorSuppl: '#df5943',
    successColor: '#167d70',
    warningColor: '#b47814',
    errorColor: '#bf4551',
    borderRadius: '7px',
    fontFamily: 'Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif',
  },
  Input: {
    ...inputControlOverrides,
  },
  Button: {
    heightSmall: '34px',
    heightMedium: '40px',
    heightLarge: '46px',
    borderRadiusSmall: '7px',
    borderRadiusMedium: '7px',
    borderRadiusLarge: '7px',
    fontWeight: '700',
  },
  InputNumber: {
    peers: {
      Input: {
        ...inputControlOverrides,
      },
    },
  },
  InternalSelection: {
    ...selectionControlOverrides,
  },
  Checkbox: {
    colorChecked: '#df5943',
    borderChecked: '1px solid #df5943',
    boxShadowFocus: '0 0 0 3px rgba(223, 89, 67, .12)',
  },
  Switch: {
    railColorActive: '#167d70',
    boxShadowFocus: '0 0 0 3px rgba(22, 125, 112, .14)',
  },
  Avatar: { borderRadius: '7px' },
  Progress: { railHeight: '7px' },
  Empty: { fontSizeMedium: '14px', iconSizeMedium: '34px' },
  Alert: { borderRadius: '8px', fontSize: '12px' },
  Tabs: { tabFontWeightActive: '700', panePaddingMedium: '0' },
}

export const lightThemeOverrides: GlobalThemeOverrides = {
  ...baseThemeOverrides,
  common: {
    ...baseThemeOverrides.common,
    bodyColor: '#f4f5f2',
    cardColor: '#ffffff',
    modalColor: '#ffffff',
    popoverColor: '#ffffff',
    inputColor: '#ffffff',
    borderColor: '#d3d5d0',
  },
  Select: {
    menuBoxShadow: '0 16px 42px rgba(28, 29, 31, .16)',
    peers: {
      InternalSelectMenu: {
        borderRadius: '7px',
        optionColorActive: '#fbeae6',
        optionColorActivePending: '#f7ddd7',
        optionTextColorActive: '#b63c2c',
        optionCheckColor: '#df5943',
      },
    },
  },
  Avatar: { ...baseThemeOverrides.Avatar, color: '#eceeea' },
  Progress: { ...baseThemeOverrides.Progress, railColor: '#e8eae6', fillColor: '#167d70' },
  Empty: { ...baseThemeOverrides.Empty, textColor: '#484b50', iconColor: '#85898e', extraTextColor: '#92959b' },
  Alert: {
    ...baseThemeOverrides.Alert,
    colorSuccess: '#ffffff',
    borderSuccess: '1px solid #bcded7',
    contentTextColorSuccess: '#145d53',
    colorError: '#ffffff',
    borderError: '1px solid #ebc4c8',
    contentTextColorError: '#8b343d',
  },
  Tabs: {
    ...baseThemeOverrides.Tabs,
    colorSegment: '#e9ebe7',
    tabColorSegment: '#ffffff',
    tabTextColorActiveSegment: '#151619',
    tabTextColorHoverSegment: '#222327',
  },
  Modal: { color: '#ffffff', boxShadow: '0 30px 90px rgba(18, 19, 21, .3)' },
}

export const darkThemeOverrides: GlobalThemeOverrides = {
  ...baseThemeOverrides,
  common: {
    ...baseThemeOverrides.common,
    primaryColor: '#ed715c',
    primaryColorHover: '#f0816e',
    primaryColorPressed: '#d85d49',
    primaryColorSuppl: '#ed715c',
    successColor: '#35aa99',
    warningColor: '#dca64a',
    errorColor: '#e36b76',
    bodyColor: '#17181a',
    cardColor: '#202123',
    modalColor: '#202123',
    popoverColor: '#242527',
    inputColor: '#202123',
    borderColor: '#45474a',
  },
  Select: {
    menuBoxShadow: '0 18px 48px rgba(0, 0, 0, .38)',
    peers: {
      InternalSelectMenu: {
        borderRadius: '7px',
        optionColorActive: '#3d2422',
        optionColorActivePending: '#4a2a27',
        optionTextColorActive: '#f0816e',
        optionCheckColor: '#ed715c',
      },
    },
  },
  Checkbox: {
    ...baseThemeOverrides.Checkbox,
    colorChecked: '#ed715c',
    borderChecked: '1px solid #ed715c',
  },
  Avatar: { ...baseThemeOverrides.Avatar, color: '#303236' },
  Progress: { ...baseThemeOverrides.Progress, railColor: '#303236', fillColor: '#35aa99' },
  Empty: { ...baseThemeOverrides.Empty, textColor: '#d7d4cf', iconColor: '#858991', extraTextColor: '#95989f' },
  Alert: {
    ...baseThemeOverrides.Alert,
    colorSuccess: '#1c2826',
    borderSuccess: '1px solid #315b54',
    contentTextColorSuccess: '#8ed1c7',
    colorError: '#2d2023',
    borderError: '1px solid #643941',
    contentTextColorError: '#eca0a8',
  },
  Tabs: {
    ...baseThemeOverrides.Tabs,
    colorSegment: '#242527',
    tabColorSegment: '#343538',
    tabTextColorActiveSegment: '#f7f5f2',
    tabTextColorHoverSegment: '#e7e5e2',
  },
  Modal: { color: '#202123', boxShadow: '0 30px 90px rgba(0, 0, 0, .52)' },
}

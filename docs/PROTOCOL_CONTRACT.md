# Generated Wire Contract

> Generated from `internal/protocol/contract.json`; DO NOT EDIT.

**Protocol version:** `2.0`

## Capabilities

- `snapshot.diff`
- `scheduler.singleflight`
- `macro.saga`
- `humanizer.seed`
- `protocol.codegen`

## Methods

| Method | Go constant | TypeScript key | Timeout |
| :--- | :--- | :--- | ---: |
| `system.ready` | `MethodSystemReady` | `systemReady` | 10000 ms |
| `system.ping` | `MethodSystemPing` | `systemPing` | 10000 ms |
| `session.status` | `MethodSessionStatus` | `sessionStatus` | 10000 ms |
| `window.list` | `MethodWindowList` | `windowList` | 10000 ms |
| `window.create` | `MethodWindowCreate` | `windowCreate` | 30000 ms |
| `window.ensure` | `MethodWindowEnsure` | `windowEnsure` | 30000 ms |
| `window.use` | `MethodWindowUse` | `windowUse` | 10000 ms |
| `window.close` | `MethodWindowClose` | `windowClose` | 10000 ms |
| `tab.list` | `MethodTabList` | `tabList` | 10000 ms |
| `tab.open` | `MethodTabOpen` | `tabOpen` | 10000 ms |
| `tab.switch` | `MethodTabSwitch` | `tabSwitch` | 10000 ms |
| `page.snap` | `MethodPageSnap` | `pageSnap` | 10000 ms |
| `page.read` | `MethodPageRead` | `pageRead` | 10000 ms |
| `page.wait` | `MethodPageWait` | `pageWait` | 45000 ms |
| `page.eval` | `MethodPageEval` | `pageEval` | 35000 ms |
| `page.attr` | `MethodPageAttr` | `pageAttr` | 10000 ms |
| `page.net` | `MethodPageNet` | `pageNet` | 10000 ms |
| `page.formstate` | `MethodPageFormState` | `pageFormState` | 10000 ms |
| `action.click` | `MethodActionClick` | `actionClick` | 60000 ms |
| `action.type` | `MethodActionType` | `actionType` | 300000 ms |
| `action.press` | `MethodActionPress` | `actionPress` | 20000 ms |
| `action.scroll` | `MethodActionScroll` | `actionScroll` | 60000 ms |
| `action.upload` | `MethodActionUpload` | `actionUpload` | 60000 ms |
| `media.shot` | `MethodMediaShot` | `mediaShot` | 10000 ms |
| `media.rec_start` | `MethodMediaRecStart` | `mediaRecStart` | 10000 ms |
| `media.rec_stop` | `MethodMediaRecStop` | `mediaRecStop` | 10000 ms |
| `media.capture` | `MethodMediaCapture` | `mediaCapture` | 30000 ms |
| `auth.sso` | `MethodAuthSSO` | `authSSO` | 60000 ms |
| `page.describe` | `MethodPageDescribe` | `pageDescribe` | 5000 ms |
| `page.resolve` | `MethodPageResolve` | `pageResolve` | 20000 ms |
| `macro.start` | `MethodMacroStart` | `macroStart` | 10000 ms |
| `macro.save` | `MethodMacroSave` | `macroSave` | 10000 ms |
| `macro.list` | `MethodMacroList` | `macroList` | 10000 ms |
| `macro.run` | `MethodMacroRun` | `macroRun` | 10000 ms |

## Error Codes

| Code | Go constant | TypeScript key |
| ---: | :--- | :--- |
| -32000 | `ErrConnection` | `connection` |
| -32001 | `ErrElementNotFound` | `elementNotFound` |
| -32002 | `ErrElementNotInteractable` | `elementNotInteractable` |
| -32003 | `ErrTabNotFound` | `tabNotFound` |
| -32004 | `ErrDebuggerDetached` | `debuggerDetached` |
| -32005 | `ErrSSOTimeout` | `ssoTimeout` |
| -32006 | `ErrUnauthorized` | `unauthorized` |
| -32007 | `ErrNavigationFailed` | `navigationFailed` |
| -32008 | `ErrMacroStepFailed` | `macroStepFailed` |
| -32009 | `ErrDispatchedUnconfirmed` | `dispatchedUnconfirmed` |
| -32010 | `ErrSnapshotStale` | `snapshotStale` |
| -32011 | `ErrFeatureDisabled` | `featureDisabled` |
| -32012 | `ErrBusy` | `busy` |
| -32600 | `ErrInvalidRequest` | `invalidRequest` |
| -32601 | `ErrMethodNotFound` | `methodNotFound` |
| -32603 | `ErrInternal` | `internal` |

## Timeout Groups

| Group | Budget | Methods |
| :--- | ---: | :--- |
| `ActionTypeTimeout` | 300000 ms | action.type |
| `ActionMediumTimeout` | 60000 ms | action.click, action.scroll, action.upload |
| `ActionPressTimeout` | 20000 ms | action.press |
| `PageWaitTimeout` | 45000 ms | page.wait |
| `MediaCaptureTimeout` | 30000 ms | media.capture |
| `PageDescribeTimeout` | 5000 ms | page.describe |
| `PageResolveTimeout` | 20000 ms | page.resolve |
| `PageEvalTimeout` | 35000 ms | page.eval |
| `AuthSSOTimeout` | 60000 ms | auth.sso |
| `WindowCreateTimeout` | 30000 ms | window.create, window.ensure |

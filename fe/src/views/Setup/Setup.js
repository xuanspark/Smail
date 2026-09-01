import './Setup.js'

import {computed, onUnmounted, reactive, ref} from 'vue'

import {ElMessage} from 'element-plus'

import lang from '../../i18n/i18n';

import axios from 'axios'

import {Plus} from '@element-plus/icons-vue'

import {http} from "@/utils/axios";

import {buildFinalURL as buildSetupFinalURL, probeService} from './SetupNavigation.js'

let setupBootstrapPromise = null

const ensureSetupSession = () => {
  if (!import.meta.env.DEV) {
    return Promise.resolve()
  }

  if (!setupBootstrapPromise) {
    setupBootstrapPromise = http.get("/__smail_setup_bootstrap").catch((error) => {
      setupBootstrapPromise = null
      if (error?.response?.status === 500 || !error?.response) {
        throw new Error(lang.setup_service_unavailable)
      }
      throw error
    })
  }

  return setupBootstrapPromise
}

const setupRequest = (payload) => ensureSetupSession().then(() => http.post("/api/v1/setup", payload)).then((response) => {
  if (!response || typeof response.errorNo !== "number") {
    throw new Error(lang.setup_failed)
  }
  return response
})

const getSetupErrorMessage = (error, fallback = lang.setup_failed) => {
  const message = typeof error?.message === "string" ? error.message.trim() : ""
  if (message && message !== "Network Error" && !/^timeout of \d+ms exceeded$/i.test(message)) {
    return message
  }
  return fallback
}

const showSetupError = (error, fallback = lang.setup_failed) => {
  const message = getSetupErrorMessage(error, fallback)
  ElMessage.error(message)
  return message
}

const assertSetupSuccess = (response, fallback = lang.setup_failed) => {
  if (response.errorNo !== 0) {
    throw new Error(response.errorMsg || fallback)
  }
  return response
}

export function usePage() {
  const waitDesc = ref(lang.wait_desc);

  const adminSettings = reactive({
    "account": "admin",
    "password": "",
    "password2": "",
    "hadSeted": false
  })

  const dbSettings = reactive({
    "type": "sqlite",
    "dsn": "./config/smail.db",
    "mode": "fields",
    "user": "",
    "host": "127.0.0.1",
    "port": "3306",
    "password": "",
    "name": "smail"
  })

  const domainSettings = reactive({
    "web_domain": "",
    "smtp_domain": "",
    "multi_domain": []
  })

  const sslSettings = reactive({
    "type": "0",
    "challenge": "dns",
    "key_path": "./config/ssl/private.key",
    "crt_path": "./config/ssl/public.crt",
    "paramsList": [],
  })

  const active = ref(0)

  const fullscreenLoading = ref(false)

  const databaseLoading = ref(false)
  const stepLoading = ref(false)

  const databaseError = ref("")

  const getDatabaseErrorMessage = (error) => {
    const message = typeof error?.message === "string" ? error.message.trim() : ""
    if (message && !/^network error$/i.test(message) && !/^timeout of \d+ms exceeded$/i.test(message) && !/^request failed with status code \d+$/i.test(message)) {
      return message
    }
    return lang.db_request_failed
  }

  const dnsChecking = ref(false)
  const dnsChallengeNotice = ref("")

  const dnsInfos = ref({})

  const hasDNSIPv6 = computed(() => Object.values(dnsInfos.value).some((records) =>
    records.some((record) => record.type === 'AAAA')
  ))

 const sslRuntime = ref({})

  const addDomain = () => {
    domainSettings.multi_domain.push([])
  }

  const setPassword = () => {
    if (adminSettings.hadSeted) {
      return getDomainConfig().then((response) => {
        if (response) {
          active.value++
        }
      })
    }

    const passwordSize = new TextEncoder().encode(adminSettings.password).length
    if (!adminSettings.account.trim()) {
      ElMessage.error(lang.err_account_required)
      return
    }
    if (!adminSettings.password) {
      ElMessage.error(lang.err_required_pwd)
      return
    }
    if (passwordSize < 8 || passwordSize > 12) {
      ElMessage.error(lang.err_admin_pwd_length)
      return
    }
    if (adminSettings.password !== adminSettings.password2) {
      ElMessage.error(lang.err_pwd_diff)
      return
    }

    return setupRequest({
      "action": "set",
      "step": "password",
      "account": adminSettings.account,
      "password": adminSettings.password
    }).then((res) => {
      assertSetupSuccess(res)
      return getDomainConfig().then((response) => {
        if (response) {
          active.value++
        }
      })
    }).catch((error) => {
      showSetupError(error)
    })
  }
  const getPassword = () => setupRequest({"action": "get", "step": "password"}).then((res) => {
    assertSetupSuccess(res, lang.db_init_failed)
    adminSettings.hadSeted = res.data != ""
    if (adminSettings.hadSeted) {
      adminSettings.account = res.data
      adminSettings.password = "*******"
      adminSettings.password2 = "*******"
    }
    return res
  })

  const getDbConfig = () => {
    return setupRequest({"action": "get", "step": "database"}).then((res) => {
      assertSetupSuccess(res, lang.db_request_failed)
      dbSettings.type = res.data.db_type;
      dbSettings.mode = res.data.db_mode || "dsn";
      dbSettings.dsn = res.data.db_dsn || "";
      dbSettings.user = res.data.db_user || "";
      dbSettings.host = res.data.db_host || "127.0.0.1";
      dbSettings.port = res.data.db_port || (dbSettings.type === "postgres" ? "5432" : "3306");
      dbSettings.password = "";
      dbSettings.name = res.data.db_name || "smail";
      return res
    }).catch((error) => {
      databaseError.value = getDatabaseErrorMessage(error)
      ElMessage.error(databaseError.value)
      return null
    })
  }
  const getDomainConfig = () => {
    return setupRequest({"action": "get", "step": "domain"}).then((res) => {
      assertSetupSuccess(res)
      domainSettings.web_domain = res.data.web_domain;
      domainSettings.smtp_domain = res.data.smtp_domain;
      domainSettings.multi_domain = Array.isArray(res.data.domains) ? res.data.domains : []
      return res
    }).catch((error) => {
      showSetupError(error)
      return null
    })
  }

  const setDbConfig = () => {
    if (databaseLoading.value) {
      return
    }
    databaseError.value = ""

    if (dbSettings.type === "sqlite" && !dbSettings.dsn) {
      dbSettings.dsn = "./config/smail.db";
    }

    const usesStructuredFields = dbSettings.type === "mysql" || dbSettings.type === "postgres"
    if (usesStructuredFields) {
      if (!dbSettings.user.trim()) {
        ElMessage.error(lang.db_user_required)
        return
      }
      if (!dbSettings.host.trim()) {
        ElMessage.error(lang.db_host_required)
        return
      }
      if (!dbSettings.port.trim()) {
        ElMessage.error(lang.db_port_required)
        return
      }
      if (!/^\d+$/.test(dbSettings.port.trim()) || Number(dbSettings.port) < 1 || Number(dbSettings.port) > 65535) {
        ElMessage.error("数据库端口必须是 1 到 65535 之间的数字")
        return
      }
      if (!dbSettings.name.trim()) {
        ElMessage.error(lang.db_name_required)
        return
      }
    } else if (!dbSettings.dsn.trim()) {
      ElMessage.error(lang.err_db_dsn_empty)
      return
    }

    databaseLoading.value = true
    const payload = {
      "action": "set",
      "step": "database",
      "db_type": dbSettings.type,
      "db_dsn": dbSettings.dsn,
      "db_mode": usesStructuredFields ? "fields" : "dsn",
      "db_user": dbSettings.user,
      "db_host": dbSettings.host,
      "db_port": dbSettings.port,
      "db_password": dbSettings.password,
      "db_name": dbSettings.name
    }

    return setupRequest(payload).then((res) => {
      assertSetupSuccess(res, lang.db_init_failed)
      return getPassword().then(() => {
        ElMessage.success(lang.db_init_success)
        active.value++
      }).catch((error) => {
        databaseError.value = getDatabaseErrorMessage(error)
        ElMessage.error(databaseError.value)
      })
    }).catch((error) => {
      databaseError.value = getDatabaseErrorMessage(error)
      ElMessage.error(databaseError.value)
    }).finally(() => {
      databaseLoading.value = false
    })
  }

  const handleDbTypeChange = (type) => {
    databaseError.value = ""
    dbSettings.dsn = ""
    dbSettings.password = ""
    if (type === "mysql" || type === "postgres") {
      dbSettings.mode = "fields"
      dbSettings.host = "127.0.0.1"
      dbSettings.port = type === "postgres" ? "5432" : "3306"
      dbSettings.name = "smail"
    } else {
      dbSettings.mode = "dsn"
      dbSettings.dsn = "./config/smail.db"
      dbSettings.host = "127.0.0.1"
      dbSettings.port = ""
      dbSettings.name = ""
    }
  }

  const getDNSConfig = () => {
    return setupRequest({"action": "get", "step": "dns"}).then((res) => {
      assertSetupSuccess(res)
      dnsInfos.value = res.data
      return res
    }).catch((error) => {
      showSetupError(error)
      return null
    })
  }

  const copyDNSValue = async (value) => {
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(value)
      } else {
        const input = document.createElement('textarea')
        input.value = value
        input.setAttribute('readonly', '')
        input.style.position = 'fixed'
        input.style.opacity = '0'
        document.body.appendChild(input)
        input.select()
        document.execCommand('copy')
        document.body.removeChild(input)
      }
      ElMessage.success(lang.dns_copy_success)
    } catch (error) {
      ElMessage.error(lang.dns_copy_failed)
    }
  }

  const clearDatabaseError = () => {
    databaseError.value = ""
  }

  const formatDNSRecordTTL = (ttl) => {
    if (ttl >= 3600 && ttl % 3600 === 0) {
      return `${ttl / 3600}${lang.dns_ttl_hour}`
    }
    if (ttl >= 60 && ttl % 60 === 0) {
      return `${ttl / 60}${lang.dns_ttl_minute}`
    }
    return `${ttl}${lang.dns_ttl_second}`
  }

  const getSSLConfig = () => {
    return setupRequest({"action": "get", "step": "ssl"}).then((res) => {
      assertSetupSuccess(res)
      sslSettings.type = res.data.type
      if (sslSettings.type === "2") {
        sslSettings.type = "0"
        sslSettings.challenge = "dns"
      }
      if (sslSettings.type === "0") {
        sslSettings.challenge = "dns"
      }
      sslRuntime.value = res.data || {}
     return res
    }).catch((error) => {
      showSetupError(error)
      return null
    })
  }

  const setSSLConfig = () => {
    fullscreenLoading.value = true;

    let sslType = sslSettings.type;
    if (sslType === "0") {
      sslType = "2"
    }

    return setupRequest({
      "action": "set",
      "step": "ssl",
      "ssl_type": sslType,
      "key_path": sslSettings.key_path,
      "crt_path": sslSettings.crt_path
    }).then((res) => {
      assertSetupSuccess(res)
      if (sslType === "2") {
        fullscreenLoading.value = false
        dnsChecking.value = true
        dnsChallengeNotice.value = ""
        return getSSLDNSParams()
      }
      if (sslType === "1") {
        window.location.href = buildFinalURL(sslRuntime.value)
        return res
      }
      checkStatus()
      return res
    }).catch((error) => {
      fullscreenLoading.value = false
      showSetupError(error)
      return null
    })
  }
  const buildFinalURL = (sslData) => buildSetupFinalURL(sslData, domainSettings.web_domain)

  let statusTimer = null
  let dnsParamsTimer = null
  let setupViewUnmounted = false
  let statusCheckRunning = false
  let statusCheckStartedAt = 0
  const statusCheckTimeout = 10 * 60 * 1000
  const dnsVerificationTimeout = 30 * 60 * 1000
  const dnsChallengeTimerDelay = 30 * 60 * 1000

  const stopStatusCheck = () => {
    if (statusTimer !== null) {
      window.clearTimeout(statusTimer)
      statusTimer = null
    }
    statusCheckRunning = false
    statusCheckStartedAt = 0
  }

  const stopDNSParamsPolling = () => {
    if (dnsParamsTimer !== null) {
      window.clearTimeout(dnsParamsTimer)
      dnsParamsTimer = null
    }
  }

  let dnsChallengeTimer = null

  const stopDNSChallengeTimer = () => {
    if (dnsChallengeTimer !== null) {
      window.clearTimeout(dnsChallengeTimer)
      dnsChallengeTimer = null
    }
  }

  onUnmounted(() => {
    setupViewUnmounted = true
    stopStatusCheck()
    stopDNSParamsPolling()
    stopDNSChallengeTimer()
  })

  const navigateToFormalService = (sslData) => {
    stopStatusCheck()
    stopDNSChallengeTimer()
    fullscreenLoading.value = false
    const target = buildFinalURL(sslData)
    if (target && target !== window.location.href) {
      window.location.href = target
    }
  }

  const scheduleStatusCheck = () => {
    if (statusTimer !== null) {
      window.clearTimeout(statusTimer)
    }
    statusTimer = window.setTimeout(() => {
      statusTimer = null
      checkStatus()
    }, 1000)
  }

  const checkStatus = async () => {
    if (statusCheckRunning) {
      return
    }
    if (!statusCheckStartedAt) {
      statusCheckStartedAt = Date.now()
    }
    const currentStatusCheckTimeout = dnsChecking.value && sslSettings.challenge === "dns"
      ? dnsVerificationTimeout
      : statusCheckTimeout
    if (Date.now() - statusCheckStartedAt > currentStatusCheckTimeout) {
      stopStatusCheck()
      fullscreenLoading.value = false
      if (dnsChecking.value && sslSettings.challenge === "dns") {
        refreshDNSChallenge()
      } else {
        ElMessage.error(lang.setup_failed)
      }
      return
    }

    statusCheckRunning = true
    let sslData = sslRuntime.value || {}
    try {
      const response = await setupRequest({"action": "get", "step": "ssl"})
      if (response.errorNo === 0) {
        sslData = response.data || sslData
        sslRuntime.value = sslData
        if (sslData.ready) {
          navigateToFormalService(sslData)
          return
        }
      }
    } catch (error) {
      // 服务切换期间失败属于预期状态。
    } finally {
      statusCheckRunning = false
    }

    if (await probeService(sslData, domainSettings.web_domain)) {
      navigateToFormalService(sslData)
      return
    }
    scheduleStatusCheck()
  }

  const setDomainConfig = () => {
    return setupRequest({
      "action": "set",
      "step": "domain",
      "web_domain": domainSettings.web_domain,
      "smtp_domain": domainSettings.smtp_domain,
      "multi_domain": domainSettings.multi_domain.join(",")
    }).then((res) => {
      assertSetupSuccess(res)
      return getDNSConfig().then((response) => {
        if (response) {
          active.value++
        }
      })
    }).catch((error) => {
      showSetupError(error)
    })
  }

  const getSSLDNSParams = () => {
    return setupRequest({"action": "getParams", "step": "ssl"}).then((res) => {
      assertSetupSuccess(res)
      sslSettings.paramsList = Array.isArray(res.data) ? res.data : []
      if (!setupViewUnmounted && dnsChecking.value && sslSettings.paramsList.length === 0) {
        stopDNSParamsPolling()
        dnsParamsTimer = window.setTimeout(() => {
          dnsParamsTimer = null
          getSSLDNSParams()
        }, 1000)
      } else if (sslSettings.paramsList.length > 0 || !dnsChecking.value) {
        stopDNSParamsPolling()
        if (sslSettings.paramsList.length > 0 && dnsChecking.value) {
          scheduleDNSChallengeRefresh()
        }
      }
      return res
    }).catch((error) => {
      stopDNSParamsPolling()
      showSetupError(error)
      return null
    })
  }

  const scheduleDNSChallengeRefresh = () => {
    stopDNSChallengeTimer()
    if (setupViewUnmounted || !dnsChecking.value) {
      return
    }

    dnsChallengeTimer = window.setTimeout(() => {
      dnsChallengeTimer = null
      refreshDNSChallenge()
    }, dnsChallengeTimerDelay)
  }

  const getDNSRefreshNotice = () => `${lang.ssl_dns_ready}（记录已刷新，请使用最新记录。）`

  const refreshDNSChallenge = () => {
    if (setupViewUnmounted || !dnsChecking.value || fullscreenLoading.value) {
      return
    }

    stopStatusCheck()
    stopDNSChallengeTimer()
    stopDNSParamsPolling()
    fullscreenLoading.value = true

    return setupRequest({
      "action": "set",
      "step": "ssl",
      "ssl_type": "2",
      "key_path": sslSettings.key_path,
      "crt_path": sslSettings.crt_path
    }).then((res) => {
      assertSetupSuccess(res)
      sslSettings.paramsList = []
      dnsChallengeNotice.value = getDNSRefreshNotice()
      return getSSLDNSParams()
    }).then((res) => {
      ElMessage.warning(getDNSRefreshNotice())
      return res
    }).catch((error) => {
      showSetupError(error)
      return null
    }).finally(() => {
      fullscreenLoading.value = false
    })
  }

  const resetDNSChallenge = () => {
    stopStatusCheck()
    stopDNSChallengeTimer()
    stopDNSParamsPolling()
    dnsChecking.value = false
    dnsChallengeNotice.value = ""
    sslSettings.paramsList = []
    fullscreenLoading.value = false
  }

  const handleSSLTypeChange = () => {
    if (dnsChecking.value) {
      resetDNSChallenge()
    }
  }

  const handleSSLChallengeChange = (challenge) => {
    if (fullscreenLoading.value) {
      return
    }

    if (challenge === "dns") {
      dnsChallengeNotice.value = ""
      return setSSLConfig()
    }

    if (dnsChecking.value) {
      resetDNSChallenge()
    }
  }

  const runStep = (action) => {
    if (stepLoading.value) {
      return
    }
    stepLoading.value = true
    return Promise.resolve()
      .then(action)
      .finally(() => {
        stepLoading.value = false
      })
  }

  const next = () => {
    if (databaseLoading.value || stepLoading.value || fullscreenLoading.value) {
      return
    }

    switch (active.value) {
      case 0:
        runStep(() => getDbConfig().then((response) => {
          if (response) {
            active.value++
          }
        }))
        break
      case 1:
        setDbConfig()
        break
      case 2:
        runStep(setPassword)
        break
      case 3:
        runStep(setDomainConfig)
        break
      case 4:
        runStep(() => getSSLConfig().then((response) => {
          if (response) {
            active.value++
          }
        }))
        break
      case 5:
        if (dnsChecking.value) {
          stopDNSChallengeTimer()
          fullscreenLoading.value = true
          waitDesc.value = lang.dns_challenge_wait
          checkStatus()
        } else {
          runStep(setSSLConfig)
        }
        break
    }
  }
  const previous = () => {
    if (active.value > 0) {
      if (active.value === 5) {
        stopStatusCheck()
        stopDNSChallengeTimer()
        stopDNSParamsPolling()
        dnsChecking.value = false
        dnsChallengeNotice.value = ""
        sslSettings.paramsList = []
        fullscreenLoading.value = false
      }
      active.value--
    }
  }

  return {
    reactive,
    ref,
    ElMessage,
    lang,
    axios,
    Plus,
    http,
    waitDesc,
    adminSettings,
    dbSettings,
    databaseLoading,
    stepLoading,
    databaseError,
    domainSettings,
    sslSettings,
    active,
    fullscreenLoading,
    dnsChecking,
    dnsChallengeNotice,
    dnsInfos,
    hasDNSIPv6,
   addDomain,
    setPassword,
    getPassword,
    getDbConfig,
    getDomainConfig,
    setDbConfig,
    handleDbTypeChange,
    handleSSLTypeChange,
    clearDatabaseError,
    getDNSConfig,
    formatDNSRecordTTL,
    copyDNSValue,
    getSSLConfig,
    setSSLConfig,
    checkStatus,
    setDomainConfig,
    getSSLDNSParams,
    handleSSLChallengeChange,
    next,
    previous
  }
}

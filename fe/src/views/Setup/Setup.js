import './Setup.js'

import {computed, reactive, ref} from 'vue'

import {ElMessage} from 'element-plus'

import lang from '../../i18n/i18n';

import axios from 'axios'

import {Plus} from '@element-plus/icons-vue'

import {http} from "@/utils/axios";

const setupRequest = (payload) => http.post("/api/v1/setup", payload).then((response) => {
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
    "challenge": "http",
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

  const dnsInfos = ref({})

  const hasDNSIPv6 = computed(() => Object.values(dnsInfos.value).some((records) =>
    records.some((record) => record.type === 'AAAA')
  ))

  const port = ref(80)
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
      sslRuntime.value = res.data || {}
      port.value = res.data?.port || res.data?.httpPort || 80
      return res
    }).catch((error) => {
      showSetupError(error)
      return null
    })
  }

  const setSSLConfig = () => {
    fullscreenLoading.value = true;

    let sslType = sslSettings.type;
    if (sslType === "0" && sslSettings.challenge === "dns") {
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
  const buildFinalURL = (sslData) => {
    const scheme = "https:"
    const configuredDomain = String(domainSettings.web_domain || "").trim()
    if (!configuredDomain) {
      return window.location.href
    }

    let target
    try {
      target = new URL(`${scheme}//${configuredDomain}`)
    } catch (error) {
      return window.location.href
    }

    const currentHost = window.location.hostname.toLowerCase()
    const targetHost = target.hostname.toLowerCase()
    const currentPort = window.location.port
    const isSameHost = currentHost === targetHost
    const targetPort = Number(sslData?.httpsPort) || 443
    const proxyMode = sslData?.tlsMode === "proxy"
    if (isSameHost && currentPort && !proxyMode) {
      target.port = targetPort > 0 ? String(targetPort) : currentPort
    } else if (targetPort > 0 && ((scheme === "http:" && targetPort !== 80) || (scheme === "https:" && targetPort !== 443))) {
      target.port = String(targetPort)
    }

    target.pathname = window.location.pathname
    target.search = window.location.search
    target.hash = window.location.hash
    return target.toString()
  }

  const checkStatus = () => {
    setupRequest({"action": "get", "step": "ssl"}).then((res) => {
      if (res.errorNo === 0 && res.data.ready) {
        window.location.href = buildFinalURL(res.data)
        return
      }
      setTimeout(checkStatus, 1000)
    }).catch(() => {
      setTimeout(checkStatus, 1000)
    })
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
      if (dnsChecking.value && sslSettings.paramsList.length === 0) {
        setTimeout(getSSLDNSParams, 1000)
      }
      return res
    }).catch((error) => {
      showSetupError(error)
      return null
    })
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
    dnsInfos,
    hasDNSIPv6,
    port,
    addDomain,
    setPassword,
    getPassword,
    getDbConfig,
    getDomainConfig,
    setDbConfig,
    handleDbTypeChange,
    clearDatabaseError,
    getDNSConfig,
    formatDNSRecordTTL,
    copyDNSValue,
    getSSLConfig,
    setSSLConfig,
    checkStatus,
    setDomainConfig,
    getSSLDNSParams,
    next,
    previous
  }
}

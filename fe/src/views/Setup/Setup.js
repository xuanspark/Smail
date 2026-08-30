import './Setup.js'

import {computed, reactive, ref} from 'vue'

import {ElMessage} from 'element-plus'

import lang from '../../i18n/i18n';

import axios from 'axios'

import {Plus} from '@element-plus/icons-vue'

import {http} from "@/utils/axios";

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
  
  const dnsChecking = ref(false)
  
  const dnsInfos = ref({})

  const hasDNSIPv6 = computed(() => Object.values(dnsInfos.value).some((records) =>
    records.some((record) => record.type === 'AAAA')
  ))
  
  const port = ref(80)
  
  const addDomain = () => {
    domainSettings.multi_domain.push([])
  }
  
  const setPassword = () => {
    if (adminSettings.hadSeted) {
      active.value++;
      getDomainConfig();
      return;
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

    http.post("/api/v1/setup", {
      "action": "set",
      "step": "password",
      "account": adminSettings.account,
      "password": adminSettings.password
    }).then((res) => {
      if (res.errorNo !== 0) {
        ElMessage.error(res.errorMsg || lang.setup_failed)
      } else {
        active.value++;
        getDomainConfig();
      }
    }).catch(() => {
      ElMessage.error(lang.setup_failed)
    })
  }
  
  const getPassword = () => {
    http.post("/api/v1/setup", {"action": "get", "step": "password"}).then((res) => {
      if (res.errorNo !== 0) {
        ElMessage.error(res.errorMsg)
      } else {
        adminSettings.hadSeted = res.data !== ""
        if (adminSettings.hadSeted) {
          adminSettings.account = res.data
          adminSettings.password = "*******"
          adminSettings.password2 = "*******"
        }
  
      }
    })
  }
  
  const getDbConfig = () => {
    http.post("/api/v1/setup", {"action": "get", "step": "database"}).then((res) => {
      if (res.errorNo !== 0) {
        ElMessage.error(res.errorMsg)
      } else {
        dbSettings.type = res.data.db_type;
        dbSettings.mode = res.data.db_mode || "dsn";
        dbSettings.dsn = res.data.db_dsn || "";
        dbSettings.user = res.data.db_user || "";
        dbSettings.host = res.data.db_host || "127.0.0.1";
        dbSettings.port = res.data.db_port || (dbSettings.type === "postgres" ? "5432" : "3306");
        dbSettings.password = "";
        dbSettings.name = res.data.db_name || "smail";
      }
    })
  }
  
  const getDomainConfig = () => {
    http.post("/api/v1/setup", {"action": "get", "step": "domain"}).then((res) => {
      if (res.errorNo !== 0) {
        ElMessage.error(res.errorMsg)
      } else {
        domainSettings.web_domain = res.data.web_domain;
        domainSettings.smtp_domain = res.data.smtp_domain;
        domainSettings.multi_domain = res.data.domains;
      }
    })
  }
  
  const setDbConfig = () => {
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

    http.post("/api/v1/setup", payload).then((res) => {
      if (res.errorNo !== 0) {
        ElMessage.error(res.errorMsg || lang.db_init_failed)
        return
      }
      ElMessage.success(lang.db_init_success)
      active.value++;
      getPassword();
    }).catch(() => {
      ElMessage.error(lang.db_init_failed)
    }).finally(() => {
      databaseLoading.value = false
    })
  }

  const handleDbTypeChange = (type) => {
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
    http.post("/api/v1/setup", {"action": "get", "step": "dns"}).then((res) => {
      if (res.errorNo !== 0) {
        ElMessage.error(res.errorMsg)
      } else {
        dnsInfos.value = res.data
      }
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
    http.post("/api/v1/setup", {"action": "get", "step": "ssl"}).then((res) => {
      if (res.errorNo !== 0) {
        ElMessage.error(res.errorMsg)
      } else {
        sslSettings.type = res.data.type
        if (sslSettings.type === "2") {
          sslSettings.type = "0"
          sslSettings.challenge = "dns"
        }
  
  
        port.value = res.data.port
      }
    })
  }
  
  const setSSLConfig = () => {
    fullscreenLoading.value = true;
  
    let sslType = sslSettings.type;
    if (sslType === "0" && sslSettings.challenge === "dns") {
      sslType = "2"
    }
  
  
    http.post("/api/v1/setup", {
      "action": "set",
      "step": "ssl",
      "ssl_type": sslType,
      "key_path": sslSettings.key_path,
      "crt_path": sslSettings.crt_path
    }).then((res) => {
      if (res.errorNo !== 0) {
        fullscreenLoading.value = false;
        ElMessage.error(res.errorMsg)
      } else {
        if (sslType === "2") {
          fullscreenLoading.value = false;
          dnsChecking.value = true;
          getSSLDNSParams();
          return
        }
        checkStatus();
      }
    })
  }
  
  const checkStatus = () => {
    http.post("/api/v1/setup", {"action": "get", "step": "ssl"}).then((res) => {
      if (res.errorNo === 0 && res.data.ready) {
        if (sslSettings.type === "1") {
          window.location.href = "http://" + domainSettings.web_domain;
        } else {
          window.location.href = "https://" + domainSettings.web_domain;
        }
        return
      }
      setTimeout(checkStatus, 1000)
    }).catch(() => {
      setTimeout(checkStatus, 1000)
    })
  }
  
  const setDomainConfig = () => {
    http.post("/api/v1/setup", {
      "action": "set",
      "step": "domain",
      "web_domain": domainSettings.web_domain,
      "smtp_domain": domainSettings.smtp_domain,
      "multi_domain": domainSettings.multi_domain.join(",")
    }).then((res) => {
      if (res.errorNo !== 0) {
        ElMessage.error(res.errorMsg)
      } else {
        active.value++;
        getDNSConfig();
      }
    })
  }
  
  const getSSLDNSParams = () => {
    http.post("/api/v1/setup", {"action": "getParams", "step": "ssl"}).then((res) => {
      if (res.errorNo !== 0) {
        ElMessage.error(res.errorMsg)
      } else {
        sslSettings.paramsList = Array.isArray(res.data) ? res.data : []
        if (dnsChecking.value && sslSettings.paramsList.length === 0) {
          setTimeout(getSSLDNSParams, 1000)
        }
      }
    })
  }
  
  const next = () => {
    switch (active.value) {
      case 0:
        active.value++
        getDbConfig();
        break
      case 1:
        setDbConfig();
        break;
      case 2:
        setPassword();
        break;
      case 3:
        setDomainConfig();
        break;
      case 4:
        getSSLConfig();
        active.value++
        break
      case 5:
        if (dnsChecking.value) {
          fullscreenLoading.value = true;
          waitDesc.value = lang.dns_challenge_wait;
          checkStatus();
        } else {
          setSSLConfig();
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

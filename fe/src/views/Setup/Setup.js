import './Setup.js'

import {reactive, ref} from 'vue'

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
    "lable": ""
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
  
  const dnsChecking = ref(false)
  
  const dnsInfos = ref({})
  
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
  
    if (adminSettings.password !== adminSettings.password2) {
      ElMessage.error(lang.err_pwd_diff)
    } else {
      http.post("/api/setup", {
        "action": "set",
        "step": "password",
        "account": adminSettings.account,
        "password": adminSettings.password
      }).then((res) => {
        if (res.errorNo !== 0) {
          ElMessage.error(res.errorMsg)
        } else {
          active.value++;
          getDomainConfig();
        }
      })
    }
  }
  
  const getPassword = () => {
    http.post("/api/setup", {"action": "get", "step": "password"}).then((res) => {
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
    http.post("/api/setup", {"action": "get", "step": "database"}).then((res) => {
      if (res.errorNo !== 0) {
        ElMessage.error(res.errorMsg)
      } else {
        dbSettings.type = res.data.db_type;
        dbSettings.dsn = res.data.db_dsn;
      }
    })
  }
  
  const getDomainConfig = () => {
    http.post("/api/setup", {"action": "get", "step": "domain"}).then((res) => {
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
    // 切换数据库类型为sqlite时，数据库路径为空，则使用默认路径
    if (dbSettings.type === "sqlite" && !dbSettings.dsn) dbSettings.dsn = "./config/smail.db";
    else if (!dbSettings.dsn) ElMessage({
      title: "Error",
      message: lang.err_db_dsn_empty,
      type: "error",
    });
    http.post("/api/setup", {
      "action": "set",
      "step": "database",
      "db_type": dbSettings.type,
      "db_dsn": dbSettings.dsn
    }).then((res) => {
      if (res.errorNo !== 0) {
        ElMessage.error(res.errorMsg)
      } else {
        active.value++;
        getPassword();
      }
    })
  }
  
  const getDNSConfig = () => {
    http.post("/api/setup", {"action": "get", "step": "dns"}).then((res) => {
      if (res.errorNo !== 0) {
        ElMessage.error(res.errorMsg)
      } else {
        dnsInfos.value = res.data
      }
    })
  }
  
  const getSSLConfig = () => {
    http.post("/api/setup", {"action": "get", "step": "ssl"}).then((res) => {
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
  
  
    http.post("/api/setup", {
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
        if (sslType === 2) {
          fullscreenLoading.value = false;
          dnsChecking.value = true;
          getSSLDNSParams();
        }
        checkStatus();
      }
    })
  }
  
  const checkStatus = () => {
    axios.post("/api/ping", {}).then((res) => {
      if (res.data.errorNo !== 0) {
        setTimeout(function () {
          checkStatus()
        }, 1000);
      } else {
        if (sslSettings.type === 1) {
          window.location.href = "http://" + domainSettings.web_domain;
        } else {
          window.location.href = "https://" + domainSettings.web_domain;
        }
      }
    }).catch(() => {
      setTimeout(function () {
        checkStatus()
      }, 1000);
    })
  }
  
  const setDomainConfig = () => {
    http.post("/api/setup", {
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
    http.post("/api/setup", {"action": "getParams", "step": "ssl"}).then((res) => {
      if (res.errorNo !== 0) {
        ElMessage.error(res.errorMsg)
      } else {
        sslSettings.paramsList = res.data
        console.log(sslSettings.paramsList)
      }
    })
  
    if (sslSettings.paramsList.length === 0) {
      setTimeout(function () {
        getSSLDNSParams()
      }, 1000);
    }
  
  
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
        } else {
          setSSLConfig();
        }
        break
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
    domainSettings,
    sslSettings,
    active,
    fullscreenLoading,
    dnsChecking,
    dnsInfos,
    port,
    addDomain,
    setPassword,
    getPassword,
    getDbConfig,
    getDomainConfig,
    setDbConfig,
    getDNSConfig,
    getSSLConfig,
    setSSLConfig,
    checkStatus,
    setDomainConfig,
    getSSLDNSParams,
    next
  }
}

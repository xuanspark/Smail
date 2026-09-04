import axios from 'axios'
import lang from '../i18n/i18n';
import {router} from "@/router";
import {createLoginRedirectHandler} from "@/utils/navigation.mjs";

//创建axios的一个实例
const http = axios.create({
    baseURL: import.meta.env.VITE_APP_URL, //接口统一域名
    timeout: 60000,
    // 初始化授权和登录会话都依赖服务端 Cookie；开发代理或反向代理跨源时也必须携带 Cookie。
    withCredentials: true,
    xsrfCookieName: 'smail_csrf',
    xsrfHeaderName: 'X-CSRF-Token',
    withXSRFToken: true, //设置超时
    headers: {
        'Content-Type': 'application/json;charset=UTF-8;',
        'Lang': lang.lang
    }
});

let redirectToLogin = null;

function handleLoginRedirect() {
    if (!redirectToLogin) {
        redirectToLogin = createLoginRedirectHandler(router);
    }
    return redirectToLogin();
}

//请求拦截器
http.interceptors.request.use((config) => {
    //若请求方式为post，则将data参数转为JSON字符串
    if (config.method === 'POST') {
        config.data = JSON.stringify(config.data);
    }
    return config;
}, (error) =>
    // 对请求错误做些什么
    Promise.reject(error));

//响应拦截器
function attachApiErrorDetails(error) {
    const payload = error?.response?.data
    const details = payload?.data

    if (payload && typeof payload === 'object') {
        error.apiErrorCode = typeof details?.code === 'string' ? details.code : ''
        error.apiRequestId = typeof details?.requestId === 'string' ? details.requestId : ''
        error.apiErrorFields = details?.fields && typeof details.fields === 'object' ? details.fields : {}
        if (typeof payload.errorMsg === 'string' && payload.errorMsg.trim()) {
            error.message = payload.errorMsg
        }
    }

    return error
}

function getRequestPath(config) {
    const rawUrl = String(config?.url || '').split('?')[0]
    try {
        const origin = typeof window !== 'undefined' ? window.location.origin : 'http://localhost'
        return new URL(rawUrl, config?.baseURL || origin).pathname
    } catch (error) {
        return rawUrl
    }
}

function isSetupRequest(config) {
    const path = getRequestPath(config)
    return path === '/api/setup' || path === '/api/v1/setup' || path === '/__smail_setup_bootstrap'
}

function isAuthenticationRefreshRequest(config) {
    const path = getRequestPath(config)
    return path === '/api/user/info' || path === '/api/v1/user/info'
}

function isAuthenticationFailureError(error) {
    return error?.response?.data?.errorNo === 403
        || (error?.response?.status === 403 && isAuthenticationRefreshRequest(error.config))
}

http.interceptors.response.use(async (response) => {
    //响应成功
    if (response.data?.errorNo === 403
        && !isSetupRequest(response.config)
        && !isAuthenticationRefreshRequest(response.config)) {
        await handleLoginRedirect();
    }
    return response.data;
}, async (error) => {
    //响应错误
    if (error.response && error.response.status) {
        let message = ""
        switch (error.response.status) {
            case 400:
                message = '请求错误';
                break;
            case 401:
                message = '请求错误';
                break;
            case 403:
                if (isSetupRequest(error.config)) {
                    message = lang.setup_failed
                } else if (isAuthenticationRefreshRequest(error.config)) {
                    message = '登录状态已失效'
                } else if (isAuthenticationFailureError(error)) {
                    await handleLoginRedirect();
                } else {
                    message = '无权访问'
                }
                break;
            case 404:
                message = '请求地址出错';
                break;
            case 408:
                message = '请求超时';
                break;
            case 500:
                message = '服务器内部错误!';
                break;
            case 501:
                message = '服务未实现!';
                break;
            case 502:
                message = '网关错误!';
                break;
            case 503:
                message = '服务不可用!';
                break;
            case 504:
                message = '网关超时!';
                break;
            case 505:
                message = 'HTTP版本不受支持';
                break;
            default:
                // eslint-disable-next-line no-unused-vars
                message = '请求失败';
        }
        const enrichedError = attachApiErrorDetails(error)
        if (message && !enrichedError.response?.data?.errorMsg) {
            enrichedError.message = message
        }
        return Promise.reject(enrichedError)
    }
    return Promise.reject(attachApiErrorDetails(error));
});

export {http};

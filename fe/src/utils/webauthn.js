function normalizeBase64Url(value) {
  return String(value || '').replace(/-/g, '+').replace(/_/g, '/');
}

export function base64UrlToBuffer(value) {
  const normalized = normalizeBase64Url(value);
  const padded = normalized + '='.repeat((4 - normalized.length % 4) % 4);
  const binary = atob(padded);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return bytes.buffer;
}

export function bufferToBase64Url(value) {
  if (value === null || value === undefined) return null;
  const bytes = value instanceof ArrayBuffer
    ? new Uint8Array(value)
    : new Uint8Array(value.buffer, value.byteOffset, value.byteLength);
  let binary = '';
  for (let index = 0; index < bytes.length; index += 1) {
    binary += String.fromCharCode(bytes[index]);
  }
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
}

function normalizeDescriptors(items) {
  if (!Array.isArray(items)) return [];
  return items.map(item => ({...item, id: base64UrlToBuffer(item.id)}));
}

export function normalizeCreationOptions(options) {
  const source = options?.publicKey || options?.response || options;
  if (!source?.challenge || !source?.user?.id) {
    throw new Error('??????????');
  }
  return {
    ...source,
    challenge: base64UrlToBuffer(source.challenge),
    user: {...source.user, id: base64UrlToBuffer(source.user.id)},
    excludeCredentials: normalizeDescriptors(source.excludeCredentials),
  };
}

export function normalizeRequestOptions(options) {
  const source = options?.publicKey || options?.response || options;
  if (!source?.challenge) {
    throw new Error('??????????');
  }
  return {
    ...source,
    challenge: base64UrlToBuffer(source.challenge),
    allowCredentials: normalizeDescriptors(source.allowCredentials),
  };
}

export function serializeCredential(credential) {
  if (!credential) throw new Error('??????????');
  const response = credential.response;
  const result = {
    id: credential.id,
    rawId: bufferToBase64Url(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment || undefined,
    clientExtensionResults: credential.getClientExtensionResults?.() || {},
    response: {
      clientDataJSON: bufferToBase64Url(response.clientDataJSON),
    },
  };
  if ('attestationObject' in response) {
    result.response.attestationObject = bufferToBase64Url(response.attestationObject);
    if (typeof response.getTransports === 'function') {
      result.response.transports = response.getTransports();
    }
  }
  if ('authenticatorData' in response) {
    result.response.authenticatorData = bufferToBase64Url(response.authenticatorData);
    result.response.signature = bufferToBase64Url(response.signature);
    result.response.userHandle = bufferToBase64Url(response.userHandle);
  }
  return result;
}

export function webAuthnSupported() {
  return typeof window !== 'undefined'
    && window.isSecureContext
    && typeof navigator.credentials?.create === 'function'
    && typeof navigator.credentials?.get === 'function'
    && typeof window.PublicKeyCredential !== 'undefined';
}

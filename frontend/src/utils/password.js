export const PASSWORD_MIN_BYTES = 8
export const PASSWORD_MAX_BYTES = 12

export function getPasswordByteLength(value) {
  return new TextEncoder().encode(value || "").length
}

export function isValidPasswordLength(value) {
  const length = getPasswordByteLength(value)
  return length >= PASSWORD_MIN_BYTES && length <= PASSWORD_MAX_BYTES
}

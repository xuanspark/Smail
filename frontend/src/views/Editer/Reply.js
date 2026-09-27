import lang from '../../i18n/i18n'

const escapeHTML = value => String(value ?? '')
  .replace(/&/g, '&amp;')
  .replace(/</g, '&lt;')
  .replace(/>/g, '&gt;')
  .replace(/"/g, '&quot;')
  .replace(/'/g, '&#39;')

const readAddresses = value => {
  const addresses = JSON.parse(value || '[]')
  if (addresses === null) return []
  if (!Array.isArray(addresses)) throw new Error(lang.fail)
  return addresses.filter(item => item && typeof item.EmailAddress === 'string' && item.EmailAddress.trim())
}

const plainBody = message => {
  if (message.text) return String(message.text)
  const template = document.createElement('template')
  template.innerHTML = String(message.html || '')
    .replace(/<br\s*\/?>|<\/(?:p|div|li|h[1-6]|tr)>/gi, '\n')
  template.content.querySelectorAll('script, style').forEach(node => node.remove())
  return template.content.textContent || ''
}

export function prepareReply(message, identity) {
  if (message.type !== 0) throw new Error(lang.fail)
  const replyTo = readAddresses(message.reply_to)
  const recipients = replyTo.length ? replyTo : [{EmailAddress: message.from_address, Name: message.from_name}]
  if (!recipients.length || recipients.some(item => !item.EmailAddress || /[\r\n]/.test(item.EmailAddress))) {
    throw new Error(lang.err_email_format)
  }
  const names = new Map(recipients.map(item => [item.EmailAddress, item.Name || '']))
  const subject = String(message.subject || '').trim() || lang.mail_no_subject
  const originalRecipients = [...readAddresses(message.to), ...readAddresses(message.cc)]
  const pickedDomain = identity.domains.find(domain => originalRecipients.some(item =>
    item.EmailAddress.toLowerCase() === `${identity.sender}@${domain}`.toLowerCase()
  )) || identity.pickedDomain
  const metadata = [
    `${lang.sender}: ${message.from_name || ''} <${message.from_address || ''}>`,
    `${lang.date}: ${message.send_date || ''}`,
    `${lang.title}: ${subject}`
  ]
  const quote = [...metadata, '', plainBody(message)].join('\n')
  return {
    recipients: [...names.keys()],
    names,
    pickedDomain,
    subject: /^re\s*:/i.test(subject) ? subject : `Re: ${subject}`,
    html: `<p><br></p><blockquote><p>${escapeHTML(quote).replace(/\r?\n/g, '<br>')}</p></blockquote>`
  }
}

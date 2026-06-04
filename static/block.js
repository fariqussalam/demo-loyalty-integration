const blockCallbacks = {
  resize: "Resize1",
  openPopup: "OpenPopup1",
  logout: "Logout1"
}

const isEmbedded = () => window.parent && window.parent !== window

const popupURLForStandaloneBlock = url => {
  const popupURL = new URL(url, window.location.origin)
  popupURL.searchParams.set("return_to", location.pathname + location.search)
  return popupURL.pathname + popupURL.search
}

const postResize = () => {
  if (!isEmbedded()) return
  parent.postMessage({
    blockClientCall: blockCallbacks.resize,
    height: Math.max(document.documentElement.scrollHeight, 360),
    title: document.title
  }, "*")
}

const requestPopup = button => {
  const popupURL = button.dataset.openPopup
  if (!isEmbedded()) {
    location.href = popupURLForStandaloneBlock(popupURL)
    return
  }
  parent.postMessage({
    blockClientCall: blockCallbacks.openPopup,
    name: popupURL.split("/").pop(),
    url: popupURL
  }, "*")
}

const resetSandboxDiscount = slug => fetch(`/storefront/${slug}/reset-discount`, {
  method: "POST",
  headers: { "Accept": "application/json" }
})

const logout = async () => {
  const url = new URL(location.href)
  url.searchParams.delete("auth")
  url.searchParams.delete("apply_code")

  const slug = document.body.dataset.sandboxSlug
  if (!isEmbedded()) {
    if (slug) await resetSandboxDiscount(slug)
    location.href = url.pathname + url.search
    return
  }

  parent.postMessage({
    blockClientCall: blockCallbacks.logout,
    url: url.pathname + url.search
  }, "*")
}

const copyText = async button => {
  await navigator.clipboard.writeText(button.dataset.copy)
  const original = button.textContent
  button.textContent = "Copied"
  setTimeout(() => button.textContent = original, 1200)
}

document.addEventListener("click", event => {
  const button = event.target.closest("[data-open-popup]")
  if (!button) return
  event.preventDefault()
  requestPopup(button)
})

document.addEventListener("click", event => {
  const button = event.target.closest("[data-logout]")
  if (!button) return
  event.preventDefault()
  logout()
})

document.addEventListener("click", event => {
  const button = event.target.closest("[data-copy]")
  if (!button) return
  event.preventDefault()
  copyText(button)
})

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", postResize, { once: true })
} else {
  postResize()
}

new ResizeObserver(postResize).observe(document.body)

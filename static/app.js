const blockCallbacks = {
  resize: "Resize1",
  openPopup: "OpenPopup1",
  closePopup: "ClosePopup1",
  logout: "Logout1"
}

const cartResetEvent = "cart:discount-reset"
const rewardsCodeMessage = "Rewards code ready. Apply it in the cart."
const redeemedCodeToast = code => `Discount code ${code} redeemed. Apply it in the cart to use it.`

const authStorageKey = slug => `rewardsAuth:${slug}`
const money = cents => `$${(cents / 100).toFixed(2)}`

const discountControls = () => ({
  input: document.querySelector("[data-discount-input]"),
  message: document.querySelector("[data-discount-message]")
})

const showToast = message => {
  let region = document.querySelector(".toast-region")
  if (!region) {
    region = document.createElement("div")
    region.className = "toast-region"
    region.setAttribute("aria-live", "polite")
    region.setAttribute("aria-atomic", "true")
    document.body.append(region)
  }

  const toast = document.createElement("div")
  toast.className = "toast"
  toast.textContent = message
  region.append(toast)

  requestAnimationFrame(() => toast.classList.add("show"))
  setTimeout(() => {
    toast.classList.remove("show")
    setTimeout(() => toast.remove(), 220)
  }, 2600)
}

const resetSandboxDiscount = async slug => {
  const response = await fetch(`/storefront/${slug}/reset-discount`, {
    method: "POST",
    headers: { "Accept": "application/json" }
  })
  if (!response.ok) return {}
  return response.json()
}

const announceDiscountReset = pendingCode => {
  window.dispatchEvent(new CustomEvent(cartResetEvent, {
    detail: { pendingCode: pendingCode || "" }
  }))
}

const restoreStoredAuth = (slug, mainFrame) => {
  if (!slug) return

  const url = new URL(mainFrame.src, window.location.origin)
  const storedAuth = localStorage.getItem(authStorageKey(slug))
  if (storedAuth && !url.searchParams.has("auth")) {
    url.searchParams.set("auth", storedAuth)
    mainFrame.src = url.pathname + url.search
  }
}

const closePopup = (dialog, popupFrame) => {
  if (dialog?.open) dialog.close()
  if (popupFrame) popupFrame.removeAttribute("src")
}

const openPopupFrame = (dialog, popupFrame, url) => {
  popupFrame.src = url
  if (typeof dialog.showModal === "function") {
    dialog.showModal()
    return
  }
  dialog.setAttribute("open", "")
}

const isTrustedFrameMessage = (event, mainFrame, popupFrame) =>
  event.source === mainFrame.contentWindow || event.source === popupFrame?.contentWindow

const rememberAuthAndResetCart = async (slug, url, auth) => {
  url.searchParams.set("auth", auth)
  if (!slug) return

  localStorage.setItem(authStorageKey(slug), auth)
  const reset = await resetSandboxDiscount(slug)
  announceDiscountReset(reset.pendingCode)
}

const showPendingRewardsCode = code => {
  const { input, message } = discountControls()
  if (input) input.value = code
  if (message) message.textContent = rewardsCodeMessage
  showToast(redeemedCodeToast(code))
}

const clearAuthAndResetCart = async (slug, mainFrame, nextURL) => {
  localStorage.removeItem(authStorageKey(slug))
  const reset = await resetSandboxDiscount(slug)
  const url = new URL(nextURL || mainFrame.src, window.location.origin)
  url.searchParams.delete("auth")
  url.searchParams.delete("apply_code")
  mainFrame.src = url.pathname + url.search

  const { input, message } = discountControls()
  if (input) input.value = reset.pendingCode || ""
  if (message) message.textContent = reset.pendingCode ? rewardsCodeMessage : ""
  announceDiscountReset(reset.pendingCode)
}

const initRewardsBridge = () => {
  const slug = document.body.dataset.sandboxSlug
  const mainFrame = document.getElementById("rewards-frame")
  const dialog = document.getElementById("app-popup")
  const popupFrame = document.getElementById("app-popup-frame")
  if (!mainFrame) return

  restoreStoredAuth(slug, mainFrame)
  document.querySelector("[data-close-popup]")?.addEventListener("click", () => closePopup(dialog, popupFrame))
  dialog?.addEventListener("close", () => popupFrame?.removeAttribute("src"))

  window.addEventListener("message", async event => {
    const data = event.data
    if (!data || !data.blockClientCall) return
    if (!isTrustedFrameMessage(event, mainFrame, popupFrame)) return

    if (data.blockClientCall === blockCallbacks.resize) {
      mainFrame.style.height = `${data.height}px`
      if (data.title && !mainFrame.title) mainFrame.title = data.title
    }

    if (data.blockClientCall === blockCallbacks.openPopup && dialog && popupFrame) {
      openPopupFrame(dialog, popupFrame, data.url)
    }

    if (data.blockClientCall === blockCallbacks.closePopup) {
      closePopup(dialog, popupFrame)
      const url = new URL(mainFrame.src, window.location.origin)
      if (data.auth) await rememberAuthAndResetCart(slug, url, data.auth)
      if (data.code) showPendingRewardsCode(data.code)
      mainFrame.src = url.pathname + url.search
    }

    if (data.blockClientCall === blockCallbacks.logout && slug) {
      await clearAuthAndResetCart(slug, mainFrame, data.url)
    }
  })
}

const readCartElements = () => ({
  count: document.querySelector("[data-qty-value]"),
  cartCount: document.querySelector("[data-cart-count]"),
  buyQuantity: document.querySelector("[data-buy-quantity]"),
  subtotal: document.getElementById("cart-subtotal"),
  discount: document.getElementById("cart-discount"),
  total: document.getElementById("cart-total"),
  discountInput: document.querySelector("[data-discount-input]"),
  discountMessage: document.querySelector("[data-discount-message]"),
  discountForm: document.querySelector("[data-discount-form]"),
  buyForm: document.querySelector("[data-buy-form]")
})

const initStorefrontCart = () => {
  const slug = document.body.dataset.sandboxSlug
  const elements = readCartElements()
  if (!elements.count || !elements.cartCount || !elements.buyQuantity || !elements.subtotal || !elements.discount || !elements.total) return

  const unitPrice = Number(document.body.dataset.productPriceCents || 0)
  let discountCents = Number(document.body.dataset.discountCents || 0)
  let appliedCode = document.body.dataset.appliedCode || ""
  let cartQuantity = Number(elements.cartCount.textContent || 1)

  const selectedQuantity = () => Math.max(1, Number(elements.count.textContent || 1))

  const renderTotals = () => {
    const subtotalCents = unitPrice * cartQuantity
    const totalCents = Math.max(0, subtotalCents - discountCents)
    elements.cartCount.textContent = String(cartQuantity)
    elements.buyQuantity.value = String(cartQuantity)
    elements.subtotal.textContent = money(subtotalCents)
    elements.discount.textContent = appliedCode ? `-${money(discountCents)}` : "None"
    elements.total.textContent = money(totalCents)
  }

  document.querySelectorAll("[data-qty-step]").forEach(button => {
    button.addEventListener("click", () => {
      const nextQuantity = selectedQuantity() + Number(button.dataset.qtyStep)
      elements.count.textContent = String(Math.min(99, Math.max(1, nextQuantity)))
    })
  })

  document.querySelector("[data-add-to-cart]")?.addEventListener("click", () => {
    cartQuantity = selectedQuantity()
    renderTotals()
    showToast("Cart updated.")
  })

  elements.discountForm?.addEventListener("submit", async event => {
    event.preventDefault()
    const code = elements.discountInput?.value.trim()
    if (!code || !slug) return

    if (elements.discountMessage) elements.discountMessage.textContent = "Applying..."
    const response = await fetch(`/storefront/${slug}/apply-discount`, {
      method: "POST",
      headers: { "Accept": "application/json", "Content-Type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({ code, quantity: String(cartQuantity) })
    })
    if (!response.ok) {
      if (elements.discountMessage) elements.discountMessage.textContent = await response.text()
      return
    }

    const data = await response.json()
    appliedCode = data.code
    discountCents = data.discountCents
    document.body.dataset.appliedCode = appliedCode
    document.body.dataset.discountCents = String(discountCents)
    elements.subtotal.textContent = data.formattedSubtotal
    elements.discount.textContent = `-${data.formattedDiscount}`
    elements.total.textContent = data.formattedTotal
    if (elements.discountInput) elements.discountInput.value = data.code
    if (elements.discountMessage) elements.discountMessage.textContent = "Discount applied."
    showToast("Discount applied.")
  })

  window.addEventListener(cartResetEvent, event => {
    appliedCode = ""
    discountCents = 0
    document.body.dataset.appliedCode = ""
    document.body.dataset.discountCents = "0"
    if (elements.discountInput) elements.discountInput.value = event.detail?.pendingCode || ""
    if (elements.discountMessage) elements.discountMessage.textContent = event.detail?.pendingCode ? rewardsCodeMessage : ""
    renderTotals()
  })

  elements.buyForm?.addEventListener("submit", async event => {
    event.preventDefault()
    const body = new URLSearchParams(new FormData(elements.buyForm))
    if (slug) {
      const storedAuth = localStorage.getItem(authStorageKey(slug))
      if (storedAuth) body.set("auth", storedAuth)
    }
    const response = await fetch(elements.buyForm.action, {
      method: "POST",
      headers: { "Accept": "application/json", "Content-Type": "application/x-www-form-urlencoded" },
      body
    })
    if (!response.ok) {
      showToast(await response.text())
      return
    }
    const data = await response.json()
    showToast(`Purchase success. Order #${data.orderId} total ${data.formattedTotal}.`)
  })

  renderTotals()
}

const initCopyButtons = () => {
  document.querySelectorAll("[data-copy]").forEach(button => {
    button.addEventListener("click", async () => {
      await navigator.clipboard.writeText(button.dataset.copy)
      const original = button.textContent
      button.textContent = "Copied"
      setTimeout(() => button.textContent = original, 1200)
    })
  })
}

initRewardsBridge()
initStorefrontCart()
initCopyButtons()

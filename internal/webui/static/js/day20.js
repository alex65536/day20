function newClock(id) {
  var target = document.getElementById(id)
  if (!target) {
    throw new Error('element not found')
  }

  function timestamp() {
    var date = new Date()
    return date.getTime() + date.getMilliseconds() / 1000
  }

  function redraw() {
    function pad(x) { return (x < 10 ? '0' : '') + x }
    var ts = Math.floor(+target.getAttribute('data-clock-msecs') / 1000)
    var sgn = ts < 0 ? '-' : ''
    ts = Math.abs(ts)
    var s = ts % 60
    var m = Math.floor(ts / 60) % 60
    var h = Math.floor(ts / 3600)
    target.textContent = sgn + h + ':' + pad(m) + ':' + pad(s)
  }

  var timer, now
  var active = target.getAttribute('data-clock-active') == 'true'
  if (active) {
    now = timestamp()
    timer = setInterval(function() {
      var nxt = timestamp()
      target.setAttribute('data-clock-msecs', +target.getAttribute('data-clock-msecs') - nxt + now)
      now = nxt
      redraw()
    }, 500)
  }
  redraw()

  return {
    stop: function() {
      if (active) {
        clearInterval(timer)
      }
    },
  }
}

function formToggle(data, opts) {
  data = data.map(function(item) {
    return item.map(function(sub) { return document.getElementById(sub) })
  })

  if (!opts) {
    opts = {}
  }
  opts = Object.assign({
    isEnabled: function(v) { return v.checked },
    hide: false,
  }, opts)

  function onChange() {
    data.forEach(function(item) {
      for (var i = 1; i < item.length; ++i) {
        var enabled = opts.isEnabled(item[0])
        if (opts.hide) {
          item[i].hidden = !enabled
        } else {
          item[i].disabled = !enabled
        }
      }
    })
  }

  onChange()
  data.forEach(function(item) {
    item[0].addEventListener('change', onChange)
  })
}

function eltToClipboard(src, sel) {
  var text = src.querySelector(sel).textContent
  navigator.clipboard.writeText(text).then(function() {}, function(err) {
    console.error('could not copy: ', err)
  })
}

function eltDownload(src, sel, fileName) {
  var text = src.querySelector(sel).textContent
  var link = document.createElement('a')
  var blob = new Blob([text], {type: 'text/plain'})
  link.setAttribute('href', URL.createObjectURL(blob))
  link.setAttribute('download', fileName)
  link.click()
}

function hrefToClipboard(src, sel) {
  var text = src.querySelector(sel).href
  navigator.clipboard.writeText(text).then(function() {}, function(err) {
    console.error('could not copy: ', err)
  })
}

// HTMX error handling.
htmx.on('htmx:beforeSwap', function(e) {
  var d = e.detail
  if (400 <= d.xhr.status && d.xhr.status <= 599) {
    var src = d.elt
    var elt
    if (src && src.classList.contains('errors')) {
      elt = src
    } else {
      elt = src.querySelector('.errors')
    }
    if (!elt) {
      elt = document.getElementById('global-errors')
    }
    if (elt) {
      d.shouldSwap = false
      d.isError = true
      elt.innerHTML = d.xhr.responseText
      return
    }
  }
})

function toggleHTMXFormSubmit(elt, disabled) {
  if (!elt.matches('form.htmx-form')) {
    return
  }
  var submit = elt.querySelector('input[type=submit], button[type=submit]')
  if (!submit) {
    return
  }
  submit.disabled = disabled
}

// Disable submit buttons on forms while the request is in-flight.
htmx.on('htmx:beforeSend', function(e) { toggleHTMXFormSubmit(e.detail.elt, true) })
htmx.on('htmx:afterRequest', function(e) { toggleHTMXFormSubmit(e.detail.elt, false) })

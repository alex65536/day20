function newClock(id) {
  var target = document.getElementById(id)
  if (!target) {
    throw new Error("element not found")
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
    target.innerHTML = sgn + h + ':' + pad(m) + ':' + pad(s)
  }

  var timer, now
  var active = target.getAttribute('data-clock-active') == "true"
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

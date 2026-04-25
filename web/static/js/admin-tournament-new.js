(function () {
  var ts = document.getElementById('team_size');
  var mg = document.getElementById('team-mode-group');
  function upd() { mg.style.display = parseInt(ts.value, 10) > 1 ? '' : 'none'; }
  ts.addEventListener('input', upd);
  upd();
}());

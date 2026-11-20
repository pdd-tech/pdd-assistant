// Только удобство форм. Все проверки и CSRF выполняются также на сервере.
(() => {
  function updateCounter(question) {
    const counter = document.querySelector(`[data-counter-for="${question.id}"]`);
    if (counter) counter.textContent = `${question.value.length} / ${question.maxLength}`;
  }
  document.querySelectorAll('[data-counted]').forEach((question) => {
    updateCounter(question);
    question.addEventListener('input', () => updateCounter(question));
  });
  document.querySelectorAll('[data-question]').forEach((button) => {
    button.addEventListener('click', () => {
      const question = document.getElementById('question');
      if (!question || question.closest('form').getAttribute('aria-busy') === 'true') return;
      question.value = button.dataset.question;
      question.setCustomValidity('');
      updateCounter(question);
      question.focus();
    });
  });
  function resetForms() {
    document.querySelectorAll('form[data-pending]').forEach((form) => {
      form.removeAttribute('aria-busy');
      const question = form.querySelector('[data-counted]');
      if (question) updateCounter(question);
      form.querySelectorAll('button[type="submit"]').forEach((button) => {
        button.disabled = false;
      });
      form.querySelector('.pending').textContent = '';
    });
  }
  window.addEventListener('pageshow', resetForms);
  document.querySelectorAll('form[data-pending]').forEach((form) => {
    const question = form.querySelector('[name="question"]');
    if (question) {
      question.addEventListener('input', () => question.setCustomValidity(''));
    }
    form.addEventListener('submit', (event) => {
      if (form.getAttribute('aria-busy') === 'true') {
        event.preventDefault();
        return;
      }
      if (question && !question.value.trim()) {
        event.preventDefault();
        question.setCustomValidity('Введите вопрос, состоящий не только из пробелов.');
        question.reportValidity();
        return;
      }
      form.setAttribute('aria-busy', 'true');
      form.querySelector('.pending').textContent = form.dataset.pending;
      form.querySelectorAll('button[type="submit"]').forEach((button) => {
        button.disabled = true;
      });
    });
  });
})();

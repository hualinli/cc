document.addEventListener('DOMContentLoaded', () => {
    const loginForm = document.getElementById('loginForm');
    const errorBox = document.getElementById('errorBox');
    const loginBtn = document.getElementById('loginBtn');
    const btnText = loginBtn.querySelector('.btn-text');
    const btnLoader = loginBtn.querySelector('.btn-loader');

    loginForm.addEventListener('submit', async (e) => {
        e.preventDefault();

        // UI Feedback: Loading state
        errorBox.style.display = 'none';
        loginBtn.disabled = true;
        btnText.style.opacity = '0.5';
        btnLoader.style.display = 'inline-block';

        const formData = new FormData(loginForm);

        try {
            const response = await fetch('/login', {
                method: 'POST',
                body: formData
            });

            const result = await response.json();

            if (response.ok && result.success) {
                // Success: Redirect
                window.location.href = result.redirect;
            } else {
                // Error: Show message
                showError(result.error || '登录失败，请检查用户名或密码');
            }
        } catch (err) {
            showError('网络请求出错，请稍后重试');
        } finally {
            // Restore button state
            loginBtn.disabled = false;
            btnText.style.opacity = '1';
            btnLoader.style.display = 'none';
        }
    });

    function showError(msg) {
        errorBox.innerText = msg;
        errorBox.style.display = 'block';
    }
});

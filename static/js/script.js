window.onload = function() {
    fixAge();
    loadingBuffer();
};


function fixAge() {
    let age_span = document.getElementById("age");
    const current_date = new Date();
    const birthday = new Date("2011-03-15")
    let age = current_date.getFullYear() - birthday.getFullYear()
    if (current_date.getMonth() < birthday.getMonth() || (current_date.getMonth() === birthday.getMonth() && current_date.getDate() < birthday.getDate())) {
        age--
    }
    age_span.innerHTML = age;
}
